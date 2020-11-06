package zstack

import "context"

func (z *ZStack) resetAdapter(ctx context.Context, resetType ResetType) (Version, error) {
	resetInd := &SysResetInd{}
	err := z.requestResponder.RequestResponse(ctx, SysResetReq{ResetType: resetType}, resetInd)
	return resetInd.Version, err
}

type ResetType uint8

const (
	Hard ResetType = 0
	Soft ResetType = 1
)

type SysResetReq struct {
	ResetType ResetType
}

const SysResetReqID uint8 = 0x00

type ResetReason uint8

const (
	PowerUp  ResetReason = 0
	External ResetReason = 1
	Watchdog ResetReason = 2
)

type SysResetInd struct {
	Reason  ResetReason
	Version Version
}

type Version struct {
	TransportRevision uint8
	ProductID         uint8
	MajorRelease      uint8
	MinorRelease      uint8
	HardwareRevision  uint8
}

func (v Version) IsV3() bool {
	return v.ProductID > 0
}

const SysResetIndID uint8 = 0x80
