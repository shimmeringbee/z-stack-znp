package zstack

import (
	"context"
	"errors"
	"fmt"
	"github.com/shimmeringbee/logwrap"
	"github.com/shimmeringbee/zigbee"
)

const DefaultRadius uint8 = 0x20

func (z *ZStack) SendApplicationMessageToNode(ctx context.Context, destinationAddress zigbee.IEEEAddress, message zigbee.ApplicationMessage, requireAck bool) error {
	network, err := z.ResolveNodeNWKAddress(ctx, destinationAddress)
	if err != nil {
		z.logger.LogError(ctx, "Failed to send AfDataRequest (application message), failed to resolve IEEE Address to Network Adddress.", logwrap.Err(err), logwrap.Datum("IEEEAddress", destinationAddress.String()))
		return err
	}

	if err := z.sem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer z.sem.Release(1)

	var transactionId uint8

	select {
	case transactionId = <-z.transactionIdStore:
		defer func() { z.transactionIdStore <- transactionId }()
	case <-ctx.Done():
		return errors.New("context expired while obtaining a free transaction ID")
	}

	request := AfDataRequest{
		DestinationAddress:  network,
		DestinationEndpoint: message.DestinationEndpoint,
		SourceEndpoint:      message.SourceEndpoint,
		ClusterID:           message.ClusterID,
		TransactionID:       transactionId,
		Options:             AfDataRequestOptions{ACKRequest: requireAck},
		Radius:              DefaultRadius,
		Data:                message.Data,
	}

	if requireAck {
		_, err = z.nodeRequest(ctx, &request, &AfDataRequestReply{}, &AfDataConfirm{}, func(i interface{}) bool {
			msg := i.(*AfDataConfirm)
			return msg.TransactionID == transactionId && msg.Endpoint == message.DestinationEndpoint
		})
	} else {
		err = z.requestResponder.RequestResponse(ctx, &request, &AfDataRequestReply{})
	}

	return err
}

type AfDataRequestOptions struct {
	Reserved0      uint8 `bcfieldwidth:"1"`
	EnableSecurity bool  `bcfieldwidth:"1"`
	DiscoveryRoute bool  `bcfieldwidth:"1"`
	ACKRequest     bool  `bcfieldwidth:"1"`
	Reserved1      uint8 `bcfieldwidth:"4"`
}

type AfDataRequest struct {
	DestinationAddress  zigbee.NetworkAddress
	DestinationEndpoint zigbee.Endpoint
	SourceEndpoint      zigbee.Endpoint
	ClusterID           zigbee.ClusterID
	TransactionID       uint8
	Options             AfDataRequestOptions
	Radius              uint8
	Data                []byte `bcsliceprefix:"8"`
}

const AfDataRequestID uint8 = 0x01

type AfDataRequestReply GenericZStackStatus

func (s AfDataRequestReply) WasSuccessful() bool {
	return s.Status == ZSuccess
}

const AfDataRequestReplyID uint8 = 0x01

type AfDataConfirm struct {
	Status        ZStackStatus
	Endpoint      zigbee.Endpoint
	TransactionID uint8
}

func (s AfDataConfirm) WasSuccessful() bool {
	return s.Status == ZSuccess
}

const AfDataConfirmID uint8 = 0x80
