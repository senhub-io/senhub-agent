package data_store

import (
	"context"
	"log"
	"time"

	"senhub-agent.go/internal/agent/services/senhub_server"
)

// Data store is responsible for storing and synchronizing data to the server.

type DataPoint struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Value     float32   `json:"value"`
}

type AddCallback func([]DataPoint) error

// DataStore is an interface for data store.
type DataStore interface {
	GetName() string
	Start(chan struct{}) error
	Shutdown(context.Context) error

	GetCallback() AddCallback
}

type dataStore struct {
	buffer       Buffer
	senhubServer senhub_server.SenhubServer
}

// NewDataStore creates a new data store.
func NewDataStore(senhubServer senhub_server.SenhubServer) DataStore {
	return &dataStore{
		buffer:       NewBuffer(),
		senhubServer: senhubServer,
	}
}

func (d *dataStore) GetName() string {
	return "DataStore"
}

func (d *dataStore) GetCallback() AddCallback {
	return func(data []DataPoint) error {
		return d.buffer.Append(data)
	}
}

func (d *dataStore) Start(quitChannel chan struct{}) error {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				err := d.doSyncData()
				if err != nil {
					log.Printf("error synchronizing data: %v", err)
				}

			case <-quitChannel:
				ticker.Stop()
				return
			}
		}
	}()

	return nil
}

func (d *dataStore) doSyncData() error {
	data := d.buffer.Sync()
	log.Printf("synchronizing data: %v", data)
	response, err := d.senhubServer.Post("/metrics", data)
	if err != nil || response.StatusCode != 200 {
		if err != nil {
			log.Printf("error synchronizing data: %v", err)
		} else {
			log.Printf("error synchronizing data: %v", response.Status)
		}
		d.buffer.AbortSync(data)
		return err
	}

	return nil
}

func (d *dataStore) Shutdown(ctx context.Context) error {
	return d.doSyncData()
}
