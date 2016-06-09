package cmd

import (
	"fmt"
	"log"
	"strings"

	"github.com/Shopify/sarama"
	"github.com/gocql/gocql"
	"github.com/golang/protobuf/proto"
)

func setupKafka() {
	kafkaConfig.brokers = strings.Split(kafkaBrokerStr, ",")
	if len(kafkaConfig.brokers) < 1 {
		log.Fatalf("At least one Kafka broker is required")
	}
	if kafkaConfig.topic == "" {
		log.Fatalf("Kafka topic is required")
	}
	kp, err := NewKafkaProducer(kafkaConfig.brokers, kafkaConfig.topic, kafkaConfig.maxOpenSends)
	if err != nil {
		log.Fatalf("Error creating Kafka producer: %v", err)
	}
	kafkaConfig.producer = kp
}

// EventBusProducer describes an object capable of publishing events somewhere
type EventBusProducer interface {
	PublishEvent(gocql.UUID, string, BuildEvent_EventType, BuildEventError_ErrorType) error
}

// KafkaProducer handles sending event messages to the configured Kafka topic
type KafkaProducer struct {
	ap    sarama.AsyncProducer
	topic string
}

// NewKafkaProducer returns a new Kafka producer object
func NewKafkaProducer(brokers []string, topic string, maxsends uint) (*KafkaProducer, error) {
	conf := sarama.NewConfig()
	conf.Net.MaxOpenRequests = int(maxsends)
	conf.Producer.Return.Errors = true
	asyncp, err := sarama.NewAsyncProducer(brokers, conf)
	if err != nil {
		return nil, err
	}
	kp := &KafkaProducer{
		ap:    asyncp,
		topic: topic,
	}
	go kp.handleErrors()
	return kp, nil
}

func (kp *KafkaProducer) handleErrors() {
	var kerr *sarama.ProducerError
	for {
		kerr = <-kp.ap.Errors()
		log.Printf("Kafka producer error: %v", kerr)
	}
}

// PublishEvent publishes a build event to the configured Kafka topic
func (kp *KafkaProducer) PublishEvent(id gocql.UUID, msg string, etype BuildEvent_EventType, errtype BuildEventError_ErrorType) error {
	berr := &BuildEventError{
		ErrorType: errtype,
	}
	berr.IsError = errtype != BuildEventError_NO_ERROR
	event := BuildEvent{
		EventType: etype,
		Error:     berr,
		BuildId:   id.String(),
		Message:   msg,
	}
	val, err := proto.Marshal(&event)
	if err != nil {
		return fmt.Errorf("error marshaling protobuf: %v", err)
	}
	pmsg := &sarama.ProducerMessage{
		Topic: kp.topic,
		Key:   sarama.ByteEncoder(id.Bytes()), // Key is build ID to preserve event order (all events of a build go to the same partition)
		Value: sarama.ByteEncoder(val),
	}
	select { // don't block if Kafka is unavailable for some reason
	case kp.ap.Input() <- pmsg:
		return nil
	default:
		return fmt.Errorf("could not publish Kafka message: channel full")
	}
}
