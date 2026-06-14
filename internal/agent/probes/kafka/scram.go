package kafka

import (
	"crypto/sha256"
	"crypto/sha512"
	"hash"

	"github.com/IBM/sarama"
	"github.com/xdg-go/scram"
)

// scramClient wraps an xdg-go/scram client to satisfy the sarama.SCRAMClient
// interface. sarama requires a generator func that returns a fresh client per
// authentication attempt.
type scramClient struct {
	*scram.Client
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

func (s *scramClient) Begin(userName, password, authzID string) error {
	var err error
	s.Client, err = s.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	s.ClientConversation = s.Client.NewConversation()
	return nil
}

func (s *scramClient) Step(challenge string) (string, error) {
	return s.ClientConversation.Step(challenge)
}

func (s *scramClient) Done() bool {
	return s.ClientConversation.Done()
}

func sha256Gen() hash.Hash { return sha256.New() }
func sha512Gen() hash.Hash { return sha512.New() }

func scramSHA256Generator() sarama.SCRAMClient {
	return &scramClient{HashGeneratorFcn: sha256Gen}
}

func scramSHA512Generator() sarama.SCRAMClient {
	return &scramClient{HashGeneratorFcn: sha512Gen}
}
