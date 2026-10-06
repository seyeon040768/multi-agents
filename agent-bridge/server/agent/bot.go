package agent

import (
	"errors"
	"reflect"
	"sync"
	"time"
)

type BotInfo struct {
	UserID   string
	Username string
}
type BotProvisioner interface {
	Create(*Agent) (*BotInfo, error)
	Update(*Agent) (*BotInfo, error)
	SetActive(string, bool) error
	Delete(string) error
}

// BotFinder recovers a bot created before an interrupted KV link write.
type BotFinder interface {
	Find(*Agent) (*BotInfo, error)
}

type BotMappingStore interface {
	LinkBot(string, string) error
	UnlinkBot(string, string) error
	AgentIDForBot(string) (string, error)
}

type ProvisioningStore interface {
	Store
	BotMappingStore
}

// Production supplies a cluster lock; tests can use the local lock.
func NewServiceWithBots(store ProvisioningStore, bots BotProvisioner, lock func(string) (func(), error)) *Service {
	if lock == nil {
		var mu sync.Mutex
		lock = func(string) (func(), error) { mu.Lock(); return mu.Unlock, nil }
	}
	return &Service{store: store, bots: bots, mappings: store, lock: lock}
}
func (s *Service) save(a *Agent) error {
	version := a.Lifecycle.Version
	a.Lifecycle.Version++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	a.Lifecycle.UpdatedAt = &now
	if err := s.store.Update(a, version); err != nil {
		a.Lifecycle.Version = version
		return err
	}
	return nil
}
func (s *Service) finish(a *Agent, operationErr error) (*Agent, error) {
	a.Runtime = AgentRuntime{Status: "ACTIVE"}
	if !a.Lifecycle.Enabled {
		a.Runtime.Status = "DISABLED"
	}
	if operationErr != nil {
		message := operationErr.Error()
		a.Runtime = AgentRuntime{Status: "ERROR", Error: &message}
	}
	if err := s.save(a); err != nil {
		return nil, err
	}
	return a, nil
}
func (s *Service) provision(a *Agent) error {
	info, err := s.bots.Create(a)
	if err != nil {
		return err
	}
	a.Messenger.UserID = &info.UserID
	a.Messenger.Username = &info.Username
	// Persist the link before any further operation can fail.
	if err := s.save(a); err != nil {
		return err
	}
	if err := s.mappings.LinkBot(info.UserID, a.ID); err != nil {
		return err
	}
	if _, err := s.bots.Update(a); err != nil {
		return err
	}
	return s.bots.SetActive(info.UserID, a.Lifecycle.Enabled)
}
func (s *Service) createWithBot(a *Agent) (*Agent, error) {
	if err := Validate(a); err != nil {
		return nil, err
	}
	unlock, err := s.lock(a.ID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	a.Lifecycle = AgentLifecycle{Enabled: true, Version: 1, CreatedAt: &now, UpdatedAt: &now}
	a.Messenger.Provider = "mattermost"
	a.Messenger.Bot = true
	a.Messenger.UserID = nil
	a.Messenger.Username = nil
	a.Runtime = AgentRuntime{Status: "PROVISIONING"}
	if err := s.store.Create(a); err != nil {
		return nil, err
	}
	err = s.provision(a)
	return s.finish(a, err)
}
func (s *Service) current(id string, version int64) (*Agent, error) {
	if version < 1 {
		return nil, &ValidationError{"INVALID_VERSION", "positive version is required"}
	}
	a, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if a.Lifecycle.Version != version {
		return nil, ErrConflict
	}
	return a, nil
}
func (s *Service) updateWithBot(id string, a *Agent, version int64) (*Agent, error) {
	if a.ID != id {
		return nil, &ValidationError{"INVALID_AGENT_ID", "agent ID cannot be changed"}
	}
	if err := Validate(a); err != nil {
		return nil, err
	}
	unlock, err := s.lock(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	old, err := s.current(id, version)
	if err != nil {
		return nil, err
	}
	profileChanged := old.Name != a.Name || old.DisplayName != a.DisplayName || old.Description != a.Description || !reflect.DeepEqual(old.Avatar, a.Avatar) || !reflect.DeepEqual(old.Messenger.Profile, a.Messenger.Profile)
	profile := a.Messenger.Profile
	a.Lifecycle = old.Lifecycle
	a.Messenger = old.Messenger
	a.Messenger.Profile = profile
	a.Runtime = old.Runtime
	if !profileChanged {
		if err := s.save(a); err != nil {
			return nil, err
		}
		return a, nil
	}
	a.Runtime = AgentRuntime{Status: "PROVISIONING"}
	if err := s.save(a); err != nil {
		return nil, err
	}
	if a.Messenger.UserID == nil {
		err = s.provision(a)
	} else {
		_, err = s.bots.Update(a)
	}
	return s.finish(a, err)
}
func (s *Service) enableWithBot(id string, enabled bool, version int64) (*Agent, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	unlock, err := s.lock(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	a, err := s.current(id, version)
	if err != nil {
		return nil, err
	}
	a.Lifecycle.Enabled = enabled
	a.Runtime = AgentRuntime{Status: "PROVISIONING"}
	if err := s.save(a); err != nil {
		return nil, err
	}
	// Also reconciles interrupted provisioning and failed profile updates.
	if a.Messenger.UserID == nil {
		err = s.provision(a)
	} else {
		_, err = s.bots.Update(a)
		if err == nil {
			err = s.mappings.LinkBot(*a.Messenger.UserID, a.ID)
		}
		if err == nil {
			err = s.bots.SetActive(*a.Messenger.UserID, enabled)
		}
	}
	return s.finish(a, err)
}
func (s *Service) deleteWithBot(id string, version int64) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	unlock, err := s.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	a, err := s.current(id, version)
	if err != nil {
		return err
	}
	if a.Messenger.UserID == nil {
		if finder, ok := s.bots.(BotFinder); ok {
			info, findErr := finder.Find(a)
			if findErr != nil {
				return findErr
			}
			if info != nil {
				a.Messenger.UserID = &info.UserID
				a.Messenger.Username = &info.Username
			}
		}
	}
	a.Runtime = AgentRuntime{Status: "DELETING"}
	if err := s.save(a); err != nil {
		return err
	}
	if a.Messenger.UserID != nil {
		if err := s.bots.Delete(*a.Messenger.UserID); err != nil {
			_, saveErr := s.finish(a, err)
			return errors.Join(err, saveErr)
		}
		if err := s.mappings.UnlinkBot(*a.Messenger.UserID, id); err != nil {
			_, saveErr := s.finish(a, err)
			return errors.Join(err, saveErr)
		}
	}
	return s.store.Delete(id, a.Lifecycle.Version)
}
