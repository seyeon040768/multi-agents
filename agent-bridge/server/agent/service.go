package agent

import "time"

type Service struct {
	store    Store
	bots     BotProvisioner
	mappings BotMappingStore
	lock     func(string) (func(), error)
}

func NewService(store Store) *Service      { return &Service{store: store} }
func (s *Service) List() ([]*Agent, error) { return s.store.List() }
func (s *Service) Get(id string) (*Agent, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	return s.store.Get(id)
}
func (s *Service) Create(a *Agent) (*Agent, error) {
	if s.bots != nil {
		return s.createWithBot(a)
	}
	if err := Validate(a); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	a.Lifecycle = AgentLifecycle{Enabled: true, Version: 1, CreatedAt: &now, UpdatedAt: &now}
	a.Messenger.Provider = "mattermost"
	a.Messenger.UserID = nil
	a.Messenger.Username = nil
	a.Messenger.Bot = true
	if err := s.store.Create(a); err != nil {
		return nil, err
	}
	return a, nil
}
func (s *Service) Update(id string, a *Agent, version int64) (*Agent, error) {
	if s.bots != nil {
		return s.updateWithBot(id, a, version)
	}
	if a.ID != id {
		return nil, &ValidationError{"INVALID_AGENT_ID", "agent ID cannot be changed"}
	}
	if err := Validate(a); err != nil {
		return nil, err
	}
	current, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if version < 1 {
		return nil, &ValidationError{"INVALID_VERSION", "positive version is required"}
	}
	if current.Lifecycle.Version != version {
		return nil, ErrConflict
	}
	a.Lifecycle = current.Lifecycle
	a.Lifecycle.Version++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	a.Lifecycle.UpdatedAt = &now
	profile := a.Messenger.Profile
	a.Messenger = current.Messenger
	a.Messenger.Profile = profile
	if err := s.store.Update(a, version); err != nil {
		return nil, err
	}
	return a, nil
}
func (s *Service) SetEnabled(id string, enabled bool, version int64) (*Agent, error) {
	if s.bots != nil {
		return s.enableWithBot(id, enabled, version)
	}
	a, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if version < 1 || a.Lifecycle.Version != version {
		return nil, ErrConflict
	}
	a.Lifecycle.Enabled = enabled
	a.Lifecycle.Version++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	a.Lifecycle.UpdatedAt = &now
	if err := s.store.Update(a, version); err != nil {
		return nil, err
	}
	return a, nil
}
func (s *Service) Delete(id string, version int64) error {
	if s.bots != nil {
		return s.deleteWithBot(id, version)
	}
	if err := ValidateID(id); err != nil {
		return err
	}
	if version < 1 {
		return &ValidationError{"INVALID_VERSION", "positive version is required"}
	}
	return s.store.Delete(id, version)
}
