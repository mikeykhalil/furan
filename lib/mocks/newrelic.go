package mocks

import (
	"net/http"
	"time"

	newrelic "github.com/newrelic/go-agent"
)

// NullNewRelicApp conforms to the newrelic.Application interface but does nothing
type NullNewRelicApp struct {
}

func (nnrapp NullNewRelicApp) StartTransaction(name string, w http.ResponseWriter, r *http.Request) newrelic.Transaction {
	return &NullNewRelicTxn{}
}

func (nnrapp NullNewRelicApp) RecordCustomEvent(eventType string, params map[string]interface{}) error {
	return nil
}

func (nnrapp NullNewRelicApp) WaitForConnection(timeout time.Duration) error {
	return nil
}

func (nnrapp NullNewRelicApp) Shutdown(timeout time.Duration) {}

// NullNewRelicTxn conforms to the newrelic.Transaction interface but does nothing
type NullNewRelicTxn struct {
}

func (nnrt NullNewRelicTxn) Header() http.Header {
	return make(http.Header)
}

func (nnrt NullNewRelicTxn) Write(b []byte) (int, error) {
	return len(b), nil
}

func (nnrt NullNewRelicTxn) WriteHeader(n int) {}

func (nnrt NullNewRelicTxn) End() error {
	return nil
}

func (nnrt NullNewRelicTxn) Ignore() error {
	return nil
}

func (nnrt NullNewRelicTxn) SetName(name string) error {
	return nil
}

func (nnrt NullNewRelicTxn) NoticeError(err error) error {
	return nil
}

func (nnrt NullNewRelicTxn) AddAttribute(key string, value interface{}) error {
	return nil
}

func (nnrt NullNewRelicTxn) StartSegmentNow() newrelic.SegmentStartTime {
	return newrelic.SegmentStartTime{}
}
