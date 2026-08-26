package protocol

import "encoding/binary"

var profileMagic = [4]byte{'B', 'D', 'P', '1'}

// ProfileBlob is the on-wire/on-disk profile format: magic, slot, length,
// payload, and a trailing wrapping-sum checksum over everything from the
// slot byte onward. Must round-trip harness/golden/profile_fixture.bin.
type ProfileBlob struct {
	Slot    byte
	Payload []byte
}

// ToBytes serializes the blob: "BDP1" | slot | len(u16le) | payload | checksum(u32le).
func (p ProfileBlob) ToBytes() []byte {
	out := make([]byte, 0, 4+1+2+len(p.Payload)+4)
	out = append(out, profileMagic[:]...)
	out = append(out, p.Slot)
	lenBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(lenBuf, uint16(len(p.Payload)))
	out = append(out, lenBuf...)
	out = append(out, p.Payload...)

	sum := checksum(out[4:])
	sumBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(sumBuf, sum)
	return append(out, sumBuf...)
}

// ProfileBlobFromBytes parses and checksum-validates a serialized blob.
func ProfileBlobFromBytes(data []byte) (ProfileBlob, error) {
	if len(data) < 11 {
		return ProfileBlob{}, errInvalidInput("profile blob too short")
	}
	if [4]byte(data[0:4]) != profileMagic {
		return ProfileBlob{}, errInvalidInput("invalid profile magic")
	}

	slot := data[4]
	length := int(binary.LittleEndian.Uint16(data[5:7]))
	payloadEnd := 7 + length
	if payloadEnd+4 > len(data) {
		return ProfileBlob{}, errInvalidInput("profile length exceeds blob size")
	}

	payload := append([]byte(nil), data[7:payloadEnd]...)
	expected := binary.LittleEndian.Uint32(data[payloadEnd : payloadEnd+4])
	actual := checksum(data[4:payloadEnd])
	if expected != actual {
		return ProfileBlob{}, errInvalidInput("checksum mismatch expected=%#x actual=%#x", expected, actual)
	}

	return ProfileBlob{Slot: slot, Payload: payload}, nil
}

func checksum(data []byte) uint32 {
	var acc uint32
	for _, b := range data {
		acc += uint32(b)
	}
	return acc
}
