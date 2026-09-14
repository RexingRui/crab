package service

import (
	"context"

	"crab-order/internal/errs"
	"crab-order/internal/model"
	"crab-order/internal/store"
)

// AddressService 提供地址簿。数据从历史订单聚合而来，不单独建客户表，
// 这样永远和实际下单信息一致。
type AddressService struct {
	st store.Store
}

func NewAddressService(st store.Store) *AddressService {
	return &AddressService{st: st}
}

const (
	defaultAddressLimit = 20
	maxAddressLimit     = 100
)

func (s *AddressService) List(ctx context.Context, keyword string, limit int) ([]model.Address, error) {
	if limit <= 0 {
		limit = defaultAddressLimit
	}
	if limit > maxAddressLimit {
		limit = maxAddressLimit
	}
	list, err := s.st.ListAddresses(ctx, keyword, limit)
	if err != nil {
		return nil, errs.Internal(err)
	}
	return list, nil
}
