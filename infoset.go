package alphacats

import (
	"encoding/binary"
	"encoding/gob"

	"github.com/timpalpant/alphacats/gamestate"
)

type InfoSetWithAvailableActions struct {
	*gamestate.InfoSet
	AvailableActions []gamestate.Action
}

func (is *InfoSetWithAvailableActions) MarshalBinary() ([]byte, error) {
	buf, err := is.InfoSet.MarshalBinary()
	if err != nil {
		return nil, err
	}

	// Append available actions.
	nInfoSetBytes := len(buf)
	for _, action := range is.AvailableActions {
		packed := gamestate.EncodeAction(action)
		buf = append(buf, packed[0])

		// Actions are "varint" encoded: we only copy the private bits
		// if they are non-zero, which is indicated by the last bit of
		// the first byte.
		if packed.HasPrivateInfo() {
			buf = append(buf, packed[1], packed[2])
		}
	}

	// Append number of available actions bytes so we can split off when unmarshaling.
	nAvailableActionBytes := len(buf) - nInfoSetBytes
	var nBuf [4]byte
	binary.LittleEndian.PutUint32(nBuf[:], uint32(nAvailableActionBytes))
	buf = append(buf, nBuf[:]...)

	return buf, nil
}

func (is *InfoSetWithAvailableActions) UnmarshalBinary(buf []byte) error {
	nAvailableActionBytes := int(binary.LittleEndian.Uint32(buf[len(buf)-4:]))
	buf = buf[:len(buf)-4]

	actionsBuf := buf[len(buf)-nAvailableActionBytes:]
	buf = buf[:len(buf)-nAvailableActionBytes]
	is.InfoSet = &gamestate.InfoSet{}
	if err := is.InfoSet.UnmarshalBinary(buf); err != nil {
		return err
	}

	for len(actionsBuf) > 0 {
		packed := gamestate.EncodedAction{}
		packed[0] = actionsBuf[0]
		actionsBuf = actionsBuf[1:]

		if packed.HasPrivateInfo() {
			packed[1] = actionsBuf[0]
			packed[2] = actionsBuf[1]
			actionsBuf = actionsBuf[2:]
		}

		is.AvailableActions = append(is.AvailableActions, packed.Decode())
	}

	return nil
}

func init() {
	gob.Register(&InfoSetWithAvailableActions{})
}