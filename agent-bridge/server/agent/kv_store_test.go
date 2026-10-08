package agent

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/require"
)

type fakeKV struct {
	mu      sync.Mutex
	values  map[string][]byte
	reject  string
	failure string
}

func newKV() *fakeKV { return &fakeKV{values: map[string][]byte{}} }
func (f *fakeKV) KVGet(key string) ([]byte, *model.AppError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.values[key]...), nil
}
func (f *fakeKV) KVCompareAndSet(key string, old, next []byte) (bool, *model.AppError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failure == key {
		return false, model.NewAppError("test", "test", nil, "KV unavailable", 500)
	}
	if f.reject == key || !bytes.Equal(f.values[key], old) {
		return false, nil
	}
	f.values[key] = append([]byte(nil), next...)
	return true, nil
}
func (f *fakeKV) KVCompareAndDelete(key string, old []byte) (bool, *model.AppError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reject == key || f.values[key] == nil || !bytes.Equal(f.values[key], old) {
		return false, nil
	}
	delete(f.values, key)
	return true, nil
}
func validAgent(id string) *Agent {
	a := DefaultAgent()
	a.ID = id
	a.Name = "Researcher"
	a.Model.Name = "gpt-5.4"
	a.Prompts.Identity = "You are a research agent."
	return &a
}
func TestLifecyclePersistenceAndVersions(t *testing.T) {
	kv := newKV()
	service := NewService(NewKVAgentStore(kv))
	a := validAgent("researcher")
	forged := "forged"
	a.Lifecycle = AgentLifecycle{Version: 99, CreatedAt: &forged}
	a.Messenger.Provider = "other"
	a.Messenger.UserID = &forged
	a.Messenger.Bot = false
	created, err := service.Create(a)
	require.NoError(t, err)
	require.EqualValues(t, 1, created.Lifecycle.Version)
	require.True(t, created.Lifecycle.Enabled)
	require.NotEqual(t, &forged, created.Lifecycle.CreatedAt)
	require.Nil(t, created.Messenger.UserID)
	require.Equal(t, "mattermost", created.Messenger.Provider)
	require.True(t, created.Messenger.Bot)
	service = NewService(NewKVAgentStore(kv))
	items, err := service.List()
	require.NoError(t, err)
	require.Len(t, items, 1)
	_, err = service.Create(validAgent("researcher"))
	require.ErrorIs(t, err, ErrAlreadyExists)
	edited, err := service.Get("researcher")
	require.NoError(t, err)
	createdAt := *edited.Lifecycle.CreatedAt
	edited.Name = "Updated"
	edited.Lifecycle.Enabled = false
	edited.Lifecycle.CreatedAt = &forged
	edited.Messenger.UserID = &forged
	edited.Messenger.Provider = "forged"
	updated, err := service.Update(edited.ID, edited, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.Lifecycle.Version)
	require.True(t, updated.Lifecycle.Enabled)
	require.Equal(t, createdAt, *updated.Lifecycle.CreatedAt)
	require.Nil(t, updated.Messenger.UserID)
	_, err = service.Update(edited.ID, edited, 1)
	require.ErrorIs(t, err, ErrConflict)
	disabled, err := service.SetEnabled(edited.ID, false, 2)
	require.NoError(t, err)
	require.False(t, disabled.Lifecycle.Enabled)
	require.EqualValues(t, 3, disabled.Lifecycle.Version)
	enabled, err := service.SetEnabled(edited.ID, true, 3)
	require.NoError(t, err)
	require.True(t, enabled.Lifecycle.Enabled)
	require.EqualValues(t, 4, enabled.Lifecycle.Version)
	require.ErrorIs(t, service.Delete(edited.ID, 3), ErrConflict)
	require.NoError(t, service.Delete(edited.ID, 4))
	items, err = service.List()
	require.NoError(t, err)
	require.Empty(t, items)
	_, err = service.Get(edited.ID)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = service.Create(validAgent(edited.ID))
	require.NoError(t, err)
	items, err = service.List()
	require.NoError(t, err)
	require.Len(t, items, 1)
}
func TestCASProtectsAfterVersionCheck(t *testing.T) {
	kv := newKV()
	store := NewKVAgentStore(kv)
	service := NewService(store)
	_, err := service.Create(validAgent("researcher"))
	require.NoError(t, err)
	kv.reject = agentKey("researcher")
	a, err := service.Get("researcher")
	require.NoError(t, err)
	_, err = service.Update(a.ID, a, 1)
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, service.Delete(a.ID, 1), ErrConflict)
	current, err := service.Get(a.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, current.Lifecycle.Version)
}
func TestConcurrentCreates(t *testing.T) {
	kv := newKV()
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := NewService(NewKVAgentStore(kv)).Create(validAgent("researcher"))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrAlreadyExists)
		}
	}
	require.Equal(t, 1, successes)
	results = make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := NewService(NewKVAgentStore(kv)).Create(validAgent(fmt.Sprintf("worker-%02d", i)))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	items, err := NewKVAgentStore(kv).List()
	require.NoError(t, err)
	require.Len(t, items, 17)
}
func TestIndexFailureNeverCreatesInvisibleAgent(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			kv := newKV()
			if failure {
				kv.failure = agentIndexKey
			} else {
				kv.reject = agentIndexKey
			}
			_, err := NewService(NewKVAgentStore(kv)).Create(validAgent("researcher"))
			require.Error(t, err)
			raw, _ := kv.KVGet(agentKey("researcher"))
			require.Empty(t, raw)
		})
	}
}
func TestValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Agent)
		code   string
	}{
		{"id", func(a *Agent) { a.ID = "A!" }, "INVALID_AGENT_ID"},
		{"name", func(a *Agent) { a.Name = " \n " }, "INVALID_AGENT_NAME"},
		{"prompt", func(a *Agent) { a.Prompts.Identity = " " }, "INVALID_PROMPT"},
		{"provider", func(a *Agent) { a.Model.Provider = "unknown" }, "INVALID_MODEL"},
		{"model", func(a *Agent) { a.Model.Name = "unknown" }, "INVALID_MODEL"},
		{"context too small", func(a *Agent) { v := int64(511); a.Context.MaxContextTokens = &v }, "INVALID_CONTEXT_BUDGET"},
		{"context too large", func(a *Agent) { v := int64(2000001); a.Context.MaxContextTokens = &v }, "INVALID_CONTEXT_BUDGET"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validAgent("researcher")
			tc.mutate(a)
			var err *ValidationError
			require.ErrorAs(t, Validate(a), &err)
			require.Equal(t, tc.code, err.Code)
		})
	}
}
