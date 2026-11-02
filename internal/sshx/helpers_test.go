package sshx

import (
	"encoding/pem"
	"strconv"
)

func pemEncode(b *pem.Block) []byte { return pem.EncodeToMemory(b) }

func itoa(i int) string { return strconv.Itoa(i) }
