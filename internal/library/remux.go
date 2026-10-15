package library

// Remux m4a (AAC en conteneur MP4) -> .aac (ADTS), sans re-encodage. La boite
// accepte le .aac : au lieu de transcoder en MP3 (qui exigerait ffmpeg), on
// extrait le flux AAC tel quel et on le re-cadre en trames ADTS. Fonctionne
// pour l'AAC-LC (profil des podcasts RF). go-mp4 fournit les tailles/positions
// des echantillons ; l'AudioSpecificConfig (ASC) est lue a la main dans l'esds.

import (
	"bytes"
	"fmt"

	mp4 "github.com/abema/go-mp4"
)

// RemuxerAACenADTS transforme un conteneur m4a AAC-LC en flux .aac (ADTS).
// Erreur explicite si ce n'est pas de l'AAC-LC (a signaler non-telechargeable).
func RemuxerAACenADTS(m4a []byte) ([]byte, error) {
	r := bytes.NewReader(m4a)
	info, err := mp4.Probe(r)
	if err != nil {
		return nil, fmt.Errorf("analyse mp4 : %w", err)
	}
	var piste *mp4.Track
	for _, t := range info.Tracks {
		if t.MP4A != nil {
			piste = t
			break
		}
	}
	if piste == nil {
		return nil, fmt.Errorf("aucune piste AAC dans le conteneur")
	}

	esds, err := esdsPisteAudio(m4a)
	if err != nil {
		return nil, err
	}
	asc, err := lireASC(esds)
	if err != nil {
		return nil, err
	}
	if len(asc) < 2 {
		return nil, fmt.Errorf("config audio (ASC) illisible")
	}
	aot := (asc[0] >> 3) & 0x1F
	freqIdx := ((asc[0] & 0x07) << 1) | (asc[1] >> 7)
	chanCfg := (asc[1] >> 3) & 0x0F
	if aot != 2 {
		return nil, fmt.Errorf("profil AAC non-LC (type d'objet %d) : remux impossible, transcodage requis", aot)
	}
	if freqIdx > 12 || chanCfg == 0 || chanCfg > 7 {
		return nil, fmt.Errorf("config audio hors normes (freqIdx=%d chan=%d)", freqIdx, chanCfg)
	}

	var out bytes.Buffer
	si := 0
	for _, ch := range piste.Chunks {
		off := ch.DataOffset
		for k := 0; k < int(ch.SamplesPerChunk) && si < len(piste.Samples); k++ {
			size := uint64(piste.Samples[si].Size)
			if size == 0 {
				// stsz a taille constante (sans table) : go-mp4 ne renseigne pas les
				// tailles. Remuxer donnerait des trames vides, donc un audio MUET.
				return nil, fmt.Errorf("tailles d'echantillons absentes (stsz a taille constante) : remux impossible")
			}
			if off+size > uint64(len(m4a)) {
				return nil, fmt.Errorf("echantillon hors limites (offset %d)", off)
			}
			// frame_length ADTS tient sur 13 bits (7 d'en-tete + charge).
			// Au-dela, l'en-tete deborderait silencieusement : on refuse.
			if 7+size > 0x1FFF {
				return nil, fmt.Errorf("echantillon trop grand pour ADTS (%d octets)", size)
			}
			ecrireADTS(&out, aot, freqIdx, chanCfg, m4a[off:off+size])
			off += size
			si++
		}
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("aucun echantillon audio extrait")
	}
	return out.Bytes(), nil
}

// esdsPisteAudio renvoie les octets de la boite esds de l'entree AAC (mp4a) :
// moov/trak/mdia/minf/stbl/stsd/mp4a/esds. Une piste video (avc1, mp4v...) a
// son propre chemin : son esds eventuel n'est jamais pris pour celui de l'audio.
func esdsPisteAudio(m4a []byte) ([]byte, error) {
	chemin := mp4.BoxPath{mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(), mp4.BoxTypeMinf(),
		mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeMp4a(), mp4.BoxTypeEsds()}
	boites, err := mp4.ExtractBox(bytes.NewReader(m4a), nil, chemin)
	if err != nil {
		return nil, fmt.Errorf("lecture de l'esds : %w", err)
	}
	if len(boites) == 0 {
		return nil, fmt.Errorf("boite esds absente de la piste audio")
	}
	bi := boites[0]
	if bi.Offset+bi.Size > uint64(len(m4a)) {
		return nil, fmt.Errorf("boite esds hors limites")
	}
	return m4a[bi.Offset : bi.Offset+bi.Size], nil
}

// lireASC extrait l'AudioSpecificConfig en marchant les descripteurs de l'esds :
// ES_Descriptor(0x03) -> DecoderConfigDescriptor(0x04) -> DecSpecificInfo(0x05).
// b est la boite esds de la piste audio (esdsPisteAudio) ; elle est reperee par
// son type "esds" en tete.
func lireASC(b []byte) ([]byte, error) {
	i := bytes.Index(b, []byte("esds"))
	if i < 0 {
		return nil, fmt.Errorf("boite esds absente")
	}
	p := i + 4 + 4 // apres le tag 'esds' + version/flags (FullBox)
	longueur := func() int {
		l := 0
		for n := 0; n < 4 && p < len(b); n++ {
			c := b[p]
			p++
			l = (l << 7) | int(c&0x7F)
			if c&0x80 == 0 {
				break
			}
		}
		return l
	}
	if p >= len(b) || b[p] != 0x03 {
		return nil, fmt.Errorf("ES_Descriptor absent")
	}
	p++
	longueur()
	// ES_ID(2) puis flags(1) : streamDependenceFlag(0x80) -> dependsOn_ES_ID(2),
	// URL_Flag(0x40) -> URLlength(1) + URL, OCRstreamFlag(0x20) -> OCR_ES_Id(2)
	// (ISO 14496-1, 7.2.6.5).
	p += 2
	if p >= len(b) {
		return nil, fmt.Errorf("ES_Descriptor tronque")
	}
	flags := b[p]
	p++
	if flags&0x80 != 0 {
		p += 2
	}
	if flags&0x40 != 0 {
		if p >= len(b) {
			return nil, fmt.Errorf("ES_Descriptor tronque")
		}
		p += 1 + int(b[p])
	}
	if flags&0x20 != 0 {
		p += 2
	}
	if p >= len(b) || b[p] != 0x04 {
		return nil, fmt.Errorf("DecoderConfigDescriptor absent")
	}
	p++
	longueur()
	p += 13 // objectType(1)+streamType(1)+bufferSize(3)+maxBitrate(4)+avgBitrate(4)
	if p >= len(b) || b[p] != 0x05 {
		return nil, fmt.Errorf("DecSpecificInfo (ASC) absent")
	}
	p++
	l := longueur()
	if l <= 0 || p+l > len(b) {
		return nil, fmt.Errorf("ASC tronquee")
	}
	return b[p : p+l], nil
}

// ecrireADTS ecrit une trame AAC precedee de son en-tete ADTS (7 octets, sans CRC).
func ecrireADTS(out *bytes.Buffer, aot, freqIdx, chanCfg byte, frame []byte) {
	frameLen := 7 + len(frame)
	profil := aot - 1 // champ ADTS "profile" = AudioObjectType - 1
	h := [7]byte{
		0xFF,
		0xF1, // syncword (suite) + MPEG-4 + layer 0 + protection_absent=1
		(profil << 6) | (freqIdx << 2) | ((chanCfg >> 2) & 0x01),
		byte((chanCfg&0x03)<<6) | byte((frameLen>>11)&0x03),
		byte((frameLen >> 3) & 0xFF),
		byte((frameLen&0x07)<<5) | 0x1F,
		0xFC,
	}
	out.Write(h[:])
	out.Write(frame)
}
