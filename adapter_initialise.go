package zstack

import (
	"context"
	"errors"
	"fmt"
	"github.com/shimmeringbee/retry"
	"github.com/shimmeringbee/zigbee"
	"reflect"
)

func (z *ZStack) Initialise(ctx context.Context, nc zigbee.NetworkConfiguration) error {
	z.NetworkProperties.PANID = nc.PANID
	z.NetworkProperties.ExtendedPANID = nc.ExtendedPANID
	z.NetworkProperties.NetworkKey = nc.NetworkKey
	z.NetworkProperties.Channel = nc.Channel

	version, err := z.waitForAdapterReset(ctx)
	if err != nil {
		return err
	}

	if valid, err := z.verifyAdapterNetworkConfig(ctx, version); err != nil {
		return err
	} else if !valid {
		if err := z.wipeAdapter(ctx); err != nil {
			return err
		}

		if err := z.makeCoordinator(ctx); err != nil {
			return err
		}

		if err := z.configureNetwork(ctx, version); err != nil {
			return err
		}
	}

	if err := z.startZigbeeStack(ctx); err != nil {
		return err
	}

	if err := z.retrieveAdapterAddresses(ctx); err != nil {
		return err
	}

	if err := z.DenyJoin(ctx); err != nil {
		return err
	}

	z.startNetworkManager()
	z.startMessageReceiver()

	return nil
}

func (z *ZStack) waitForAdapterReset(ctx context.Context) (Version, error) {
	retVersion := Version{}

	err := retry.Retry(ctx, DefaultZStackTimeout, 18, func(invokeCtx context.Context) error {
		version, err := z.resetAdapter(invokeCtx, Soft)
		retVersion = version
		return err
	})

	return retVersion, err
}

func (z *ZStack) verifyAdapterNetworkConfig(ctx context.Context, version Version) (bool, error) {
	configToVerify := []interface{}{
		&ZCDNVLogicalType{LogicalType: zigbee.Coordinator},
		&ZCDNVPANID{PANID: z.NetworkProperties.PANID},
		&ZCDNVExtPANID{ExtendedPANID: z.NetworkProperties.ExtendedPANID},
		&ZCDNVChanList{Channels: channelToBits(z.NetworkProperties.Channel)},
	}

	for _, expectedConfig := range configToVerify {
		configType := reflect.TypeOf(expectedConfig).Elem()
		actualConfig := reflect.New(configType).Interface()

		if err := z.readNVRAM(ctx, actualConfig); err != nil {
			return false, err
		}

		if !reflect.DeepEqual(expectedConfig, actualConfig) {
			return false, nil
		}
	}

	return true, nil
}

func (z *ZStack) wipeAdapter(ctx context.Context) error {
	return retryFunctions(ctx, []func(context.Context) error{
		func(invokeCtx context.Context) error {
			return z.writeNVRAM(invokeCtx, ZCDNVStartUpOption{StartOption: 0x03})
		},
		func(invokeCtx context.Context) error {
			_, err := z.resetAdapter(invokeCtx, Soft)
			return err
		},
	})
}

func (z *ZStack) makeCoordinator(ctx context.Context) error {
	return retryFunctions(ctx, []func(context.Context) error{
		func(invokeCtx context.Context) error {
			return z.writeNVRAM(invokeCtx, ZCDNVLogicalType{LogicalType: zigbee.Coordinator})
		},
		func(invokeCtx context.Context) error {
			_, err := z.resetAdapter(invokeCtx, Soft)
			return err
		},
	})
}

func (z *ZStack) configureNetwork(ctx context.Context, version Version) error {
	if err := retryFunctions(ctx, []func(context.Context) error{
		func(invokeCtx context.Context) error {
			return z.writeNVRAM(invokeCtx, ZCDNVSecurityMode{Enabled: 1})
		},
		func(invokeCtx context.Context) error {
			return z.writeNVRAM(invokeCtx, ZCDNVPreCfgKeysEnable{Enabled: 1})
		},
		func(invokeCtx context.Context) error {
			return z.writeNVRAM(invokeCtx, ZCDNVPreCfgKey{NetworkKey: z.NetworkProperties.NetworkKey})
		},
		func(invokeCtx context.Context) error {
			return z.writeNVRAM(invokeCtx, ZCDNVZDODirectCB{Enabled: 1})
		},
		func(invokeCtx context.Context) error {
			return z.writeNVRAM(invokeCtx, ZCDNVChanList{Channels: channelToBits(z.NetworkProperties.Channel)})
		},
		func(invokeCtx context.Context) error {
			return z.writeNVRAM(invokeCtx, ZCDNVPANID{PANID: z.NetworkProperties.PANID})
		},
		func(invokeCtx context.Context) error {
			return z.writeNVRAM(invokeCtx, ZCDNVExtPANID{ExtendedPANID: z.NetworkProperties.ExtendedPANID})
		},
	}); err != nil {
		return err
	}

	if !version.IsV3() {
		/* Z-Stack 3.X.X has a valid default Trust Centre key, so this is not required. */
		return retryFunctions(ctx, []func(context.Context) error{
			func(invokeCtx context.Context) error {
				return z.writeNVRAM(invokeCtx, ZCDNVUseDefaultTCLK{Enabled: 1})
			},
			func(invokeCtx context.Context) error {
				return z.writeNVRAM(invokeCtx, ZCDNVTCLKTableStart{
					Address:        zigbee.IEEEAddress(0xffffffffffffffff),
					NetworkKey:     zigbee.TCLinkKey,
					TXFrameCounter: 0,
					RXFrameCounter: 0,
				})
			},
		})
	} else {
		/* Z-Stack 3.X.X requires configuration of Base Device Behaviour. */

		// TODO

		return nil
	}
}

func (z *ZStack) retrieveAdapterAddresses(ctx context.Context) error {
	return retryFunctions(ctx, []func(context.Context) error{
		func(invokeCtx context.Context) error {
			address, err := z.GetAdapterIEEEAddress(ctx)

			if err != nil {
				return err
			}

			z.NetworkProperties.IEEEAddress = address

			return nil
		},
		func(ctx context.Context) error {
			address, err := z.GetAdapterNetworkAddress(ctx)

			if err != nil {
				return err
			}

			z.NetworkProperties.NetworkAddress = address

			return nil
		},
	})
}

func (z *ZStack) startZigbeeStack(ctx context.Context) error {
	if err := retry.Retry(ctx, DefaultZStackTimeout, DefaultZStackRetries, func(invokeCtx context.Context) error {
		return z.requestResponder.RequestResponse(invokeCtx, SAPIZBStartRequest{}, &SAPIZBStartRequestReply{})
	}); err != nil {
		return err
	}

	ch := make(chan bool, 1)
	defer close(ch)

	err, cancel := z.subscriber.Subscribe(&ZDOStateChangeInd{}, func(v interface{}) {
		stateChange := v.(*ZDOStateChangeInd)

		if stateChange.State == DeviceZBCoordinator {
			ch <- true
		}
	})
	defer cancel()

	if err != nil {
		return err
	}

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return errors.New("context expired while waiting for adapter start up")
	}
}

func retryFunctions(ctx context.Context, funcs []func(context.Context) error) error {
	for _, f := range funcs {
		if err := retry.Retry(ctx, DefaultZStackTimeout, DefaultZStackRetries, f); err != nil {
			return fmt.Errorf("failed during configuration and initialisation: %w", err)
		}
	}

	return nil
}

func channelToBits(channel uint8) [4]byte {
	channelBits := 1 << channel

	channelBytes := [4]byte{}
	channelBytes[0] = byte((channelBits >> 0) & 0xff)
	channelBytes[1] = byte((channelBits >> 8) & 0xff)
	channelBytes[2] = byte((channelBits >> 16) & 0xff)
	channelBytes[3] = byte((channelBits >> 24) & 0xff)

	return channelBytes
}

type SAPIZBStartRequest struct{}

const SAPIZBStartRequestID uint8 = 0x00

type SAPIZBStartRequestReply struct{}

const SAPIZBStartRequestReplyID uint8 = 0x00

type ZBStartStatus uint8

const (
	ZBSuccess ZBStartStatus = 0x00
	ZBInit    ZBStartStatus = 0x22
)

type ZDOState uint8

const (
	DeviceCoordinatorStarting ZDOState = 0x08
	DeviceZBCoordinator       ZDOState = 0x09
)

type ZDOStateChangeInd struct {
	State ZDOState
}

const ZDOStateChangeIndID uint8 = 0xc0
