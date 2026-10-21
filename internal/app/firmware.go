package app

import (
	"log"

	"github.com/louis49/merlinus/internal/device"
)

// firmwareMinimum : seule version validee en reel (format pl.bin / pl_enc.bin de
// 96 octets, chiffrement, clean_sd.cmd). Les versions anterieures (ex. 2.1.16,
// issue #2) ne publient pas de playlist lisible par Merlinus.
var firmwareMinimum = device.Firmware{Major: 2, Minor: 1, Patch: 23}

// controlerFirmware lit la version (op5) d'une boite a associer. Bloquant si la
// version est illisible ou anterieure au minimum ; une version plus recente est
// acceptee (non testee : simple ligne de journal).
func controlerFirmware(c *device.Client) (device.Firmware, error) {
	v, err := c.FirmwareVersion()
	if err != nil {
		return v, errDetail("err_firmware_illisible", err)
	}
	switch v.Comparer(firmwareMinimum) {
	case -1:
		return v, errIHM("err_firmware_ancien", "version", v.String(), "minimum", firmwareMinimum.String())
	case 1:
		log.Printf("firmware %s plus recent que la version validee %s : poursuite", v, firmwareMinimum)
	}
	return v, nil
}
