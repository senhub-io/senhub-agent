// senhub-agent/internal/agent/services/data_store/buffer.go
package data_store

import (
	"senhub-agent.go/internal/agent/types/datapoint"
	"sync"
)

type Buffer interface {
	// Append appends data to the buffer
	Append(newData []datapoint.DataPoint) error
	// Flush the buffer data and return the data
	Sync() []datapoint.DataPoint
	// Revert the sync operation
	AbortSync(failedData []datapoint.DataPoint) error
}

type buffer struct {
	data  *[]datapoint.DataPoint
	mutex sync.Mutex // Ensures thread-safe execution of doRefreshConfig
}

func NewBuffer() Buffer {
	return &buffer{
		data: &[]datapoint.DataPoint{},
	}
}

func (b *buffer) Append(newData []datapoint.DataPoint) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	*b.data = append(*b.data, newData...)
	return nil
}

func (b *buffer) Sync() []datapoint.DataPoint {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	data := *b.data
	*b.data = []datapoint.DataPoint{}
	return data
}

func (b *buffer) AbortSync(failedData []datapoint.DataPoint) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	*b.data = append(*b.data, failedData...)
	return nil
}
