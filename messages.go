package zstack

import (
	"context"
	"errors"
	"fmt"
	"github.com/shimmeringbee/bytecodec"
	"github.com/shimmeringbee/unpi"
	"reflect"
)

func (z *ZNP) MessageRequestResponse(ctx context.Context, req interface{}, resp interface{}) error {
	reqIdentity, reqFound := z.messageLibrary.GetByObject(req)
	respIdentity, respFound := z.messageLibrary.GetByObject(resp)

	if !reqFound || !respFound {
		return errors.New("message has not been recognised")
	}

	requestPayload, err := bytecodec.Marshall(req)

	if err != nil {
		return err
	}

	requestFrame := unpi.Frame{
		MessageType: reqIdentity.MessageType,
		Subsystem:   reqIdentity.Subsystem,
		CommandID:   reqIdentity.CommandID,
		Payload:     requestPayload,
	}

	responseFrame := unpi.Frame{}

	if reqIdentity.MessageType == unpi.SREQ {
		responseFrame, err = z.SyncRequest(ctx, requestFrame)
	} else {
		if err := z.AsyncRequest(requestFrame); err != nil {
			return err
		}

		responseFrame, err = z.WaitForFrame(ctx, respIdentity.MessageType, respIdentity.Subsystem, respIdentity.CommandID)
	}

	if err != nil {
		return fmt.Errorf("bad sync request: %+v", err)
	}

	err = bytecodec.Unmarshall(responseFrame.Payload, resp)

	if err != nil {
		return nil
	}

	return nil
}

type MessageLibrary struct {
	identityToType map[MessageIdentity]reflect.Type
	typeToIdentity map[reflect.Type]MessageIdentity
}

type MessageIdentity struct {
	MessageType unpi.MessageType
	Subsystem   unpi.Subsystem
	CommandID   uint8
}

func PopulateMessageLibrary() MessageLibrary {
	cl := MessageLibrary{
		identityToType: make(map[MessageIdentity]reflect.Type),
		typeToIdentity: make(map[reflect.Type]MessageIdentity),
	}

	cl.Add(unpi.AREQ, unpi.SYS, SysResetRequestID, reflect.TypeOf(SysResetReq{}))
	cl.Add(unpi.AREQ, unpi.SYS, SysResetIndicationCommandID, reflect.TypeOf(SysResetInd{}))

	cl.Add(unpi.SREQ, unpi.SYS, SysOSALNVWriteRequestID, reflect.TypeOf(SysOSALNVWrite{}))
	cl.Add(unpi.SRSP, unpi.SYS, SysOSALNVWriteResponseID, reflect.TypeOf(SysOSALNVWriteResponse{}))

	return cl
}

func (cl *MessageLibrary) Add(messageType unpi.MessageType, subsystem unpi.Subsystem, commandID uint8, t reflect.Type) {
	identity := MessageIdentity{
		MessageType: messageType,
		Subsystem:   subsystem,
		CommandID:   commandID,
	}

	cl.identityToType[identity] = t
	cl.typeToIdentity[t] = identity
}

func (cl *MessageLibrary) GetByIdentifier(messageType unpi.MessageType, subsystem unpi.Subsystem, commandID uint8) (reflect.Type, bool) {
	identity := MessageIdentity{
		MessageType: messageType,
		Subsystem:   subsystem,
		CommandID:   commandID,
	}

	t, found := cl.identityToType[identity]
	return t, found
}

func (cl *MessageLibrary) GetByObject(v interface{}) (MessageIdentity, bool) {
	t := reflect.TypeOf(v)

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	identity, found := cl.typeToIdentity[t]
	return identity, found
}

type ResetType uint8

const (
	Hard ResetType = 0
	Soft ResetType = 1
)

type SysResetReq struct {
	ResetType ResetType
}

const SysResetRequestID uint8 = 0x00

type ResetReason uint8

const (
	PowerUp  ResetReason = 0
	External ResetReason = 1
	Watchdog ResetReason = 2
)

type SysResetInd struct {
	Reason            ResetReason
	TransportRevision uint8
	ProductID         uint8
	MajorRelease      uint8
	MinorRelease      uint8
	HardwareRevision  uint8
}

const SysResetIndicationCommandID uint8 = 0x80

type SysOSALNVWrite struct {
	NVItemID uint16
	Offset   uint8
	Value    []byte `bclength:"8"`
}

const SysOSALNVWriteRequestID uint8 = 0x09

type SysOSALNVWriteResponse struct {
	Status uint8
}

const SysOSALNVWriteResponseID uint8 = 0x09
