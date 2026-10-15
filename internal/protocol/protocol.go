// Package protocol implemente la trame du protocole local de l'enceinte Merlin
// et son CRC. Il est agnostique du transport : encoder/decoder une trame, rien
// de plus. La couche reseau vit dans internal/device.
//
// Trame, dans les deux sens : [len:1][opcode:1][payload][crc32:4 LE], ou
// len = len(opcode+payload+crc) = len(payload)+5. CRC-32/MPEG-2 calcule sur
// opcode+payload. Tous les entiers multi-octets sont little-endian.
package protocol

import (
	"encoding/binary"
	"fmt"
)

// Opcodes utiles au projet. Les opcodes 7 (firmware), 10 (restart) et 16/18
// (wifi) sont volontairement absents ; 15 (delete) sert a la restauration et a
// la suppression d'un pl.bin perime.
const (
	OpUploadFile      byte = 1
	OpPing            byte = 2
	OpSizeAvailable   byte = 3
	OpSDSize          byte = 4
	OpFirmwareVersion byte = 5
	OpUpdatePlaylist  byte = 6
	OpEndSync         byte = 9
	OpNumberOfFiles   byte = 11 // 0x0b
	OpFileInformation byte = 12 // 0x0c
	OpGetFile         byte = 13 // 0x0d : lecture d'un fichier (ex. playlist.bin)
	OpBattery         byte = 14
	OpDeleteFile      byte = 15 // 0x0f : suppression d'un fichier a la racine (valide en reel)
	OpGetWifiConfig   byte = 17 // 0x11 : configuration Wi-Fi stockee (SSID/pass)
	OpMAC             byte = 30 // 0x1e : adresse MAC de la SoftAP
	OpIsFileExist     byte = 31 // 0x1f : presence d'un fichier a la racine (+ sha)
)

// MaxPayload : le champ de longueur tient sur un octet, donc len(payload)+5 <= 255.
const MaxPayload = 250

// OpErreur est l'opcode renvoye par l'enceinte quand elle rejette une trame
// (CRC invalide de son point de vue).
const OpErreur byte = 0xFF

// CRC32MPEG2 calcule le CRC-32/MPEG-2 : polynome 0x04C11DB7, init 0xFFFFFFFF,
// MSB-first (non reflechi), sans xorout. Controle de reference pour "123456789"
// : 0x0376E6E7.
func CRC32MPEG2(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// BuildFrame construit la trame complete pour un opcode et son payload.
func BuildFrame(opcode byte, payload []byte) ([]byte, error) {
	if opcode < 1 || opcode > 33 {
		return nil, fmt.Errorf("opcode hors plage [1..33] : %d", opcode)
	}
	if len(payload) > MaxPayload {
		return nil, fmt.Errorf("payload de %d octets : maximum %d", len(payload), MaxPayload)
	}
	body := make([]byte, 0, 1+len(payload))
	body = append(body, opcode)
	body = append(body, payload...)

	frame := make([]byte, 0, 1+len(body)+4)
	frame = append(frame, byte(len(body)+4)) // len = opcode+payload+crc
	frame = append(frame, body...)
	frame = binary.LittleEndian.AppendUint32(frame, CRC32MPEG2(body))
	return frame, nil
}

// ParseFrame decompose une trame complete (octet de longueur inclus), verifie
// son CRC, et retourne l'opcode et le payload.
func ParseFrame(frame []byte) (opcode byte, payload []byte, err error) {
	if len(frame) < 6 {
		return 0, nil, fmt.Errorf("trame trop courte : %d octets", len(frame))
	}
	declared := int(frame[0])
	if declared < 5 {
		return 0, nil, fmt.Errorf("octet de longueur aberrant : %d", declared)
	}
	if len(frame) != 1+declared {
		return 0, nil, fmt.Errorf("longueur incoherente : annoncee %d, recue %d", 1+declared, len(frame))
	}
	body := frame[1 : len(frame)-4]
	crcRecu := binary.LittleEndian.Uint32(frame[len(frame)-4:])
	if crc := CRC32MPEG2(body); crc != crcRecu {
		return 0, nil, fmt.Errorf("CRC invalide : calcule %08x, recu %08x", crc, crcRecu)
	}
	return body[0], body[1:], nil
}
