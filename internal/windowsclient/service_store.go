package windowsclient

import (
	"context"
	"quiclab/internal/vpnclient"
	"reflect"
	"slices"
)

// Provisioned engine settings are authoritative; fixture imports stay untouched.
func serviceConfigStore(initial *vpnclient.Config, saved ConfigStore) ConfigStore {
	if initial == nil {
		return saved
	}
	return fixedConfigStore{cloneServiceConfig(*initial)}
}

type fixedConfigStore struct{ config vpnclient.Config }

func cloneServiceConfig(c vpnclient.Config) vpnclient.Config {
	c.Profiles.Exits = slices.Clone(c.Profiles.Exits)
	c.Profiles.Profiles = slices.Clone(c.Profiles.Profiles)
	return c
}
func (s fixedConfigStore) Load(context.Context) (*vpnclient.Config, error) {
	c := cloneServiceConfig(s.config)
	return &c, nil
}
func (s fixedConfigStore) Save(_ context.Context, c vpnclient.Config) error {
	if !reflect.DeepEqual(c, s.config) {
		return vpnclient.ErrInvalidConfig
	}
	return nil
}
