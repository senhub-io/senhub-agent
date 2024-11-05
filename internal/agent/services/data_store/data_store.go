package data_store

import "context"

// Data store is responsible for storing and synchronizing data to the server.

type DataPoint struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type AddCallback func([]DataPoint) error

// DataStore is an interface for data store.
type DataStore interface {
	GetName() string
	Start() error
	Shutdown(context.Context) error

	GetCallback() AddCallback
}

type dataStore struct {
	buffer Buffer
}

// NewDataStore creates a new data store.
func NewDataStore() DataStore {
	return &dataStore{
		buffer: NewBuffer(),
	}
}

func (d dataStore) GetName() string {
	return "DataStore"
}

func (d dataStore) GetCallback() AddCallback {
	return func(data []DataPoint) error {
		return d.buffer.Append(data)
	}
}

func (d dataStore) Start() error {
	return nil
}

func (d dataStore) Shutdown(ctx context.Context) error {
	return nil
}
