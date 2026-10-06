package agent

type Store interface {
	Create(*Agent) error
	Get(string) (*Agent, error)
	List() ([]*Agent, error)
	Update(*Agent, int64) error
	Delete(string, int64) error
}
