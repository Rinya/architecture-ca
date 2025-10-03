package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

// Схемы из спецификации (сокращённо для примера)

type UserEvent struct {
	UserID    int    `json:"user_id"`
	Username  string `json:"username,omitempty"`
	Email     string `json:"email,omitempty"`
	Action    string `json:"action"`
	Timestamp string `json:"timestamp"`
}

type PaymentEvent struct {
	PaymentID  int     `json:"payment_id"`
	UserID     int     `json:"user_id"`
	Amount     float64 `json:"amount"`
	Status     string  `json:"status"`
	Timestamp  string  `json:"timestamp"`
	MethodType string  `json:"method_type,omitempty"`
}

type MovieEvent struct {
	MovieID    int      `json:"movie_id"`
	Title      string   `json:"title"`
	Action     string   `json:"action"`
	UserID     int      `json:"user_id,omitempty"`
	Rating     float64  `json:"rating,omitempty"`
	Genres     []string `json:"genres,omitempty"`
	Description string  `json:"description,omitempty"`
}

type EventResponse struct {
	Status    string      `json:"status"`
	Partition int         `json:"partition"`
	Offset    int64       `json:"offset"`
	Event     interface{} `json:"event"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

var (
	kafkaBroker string

	userTopic    = "user-events"
	paymentTopic = "payment-events"
	movieTopic   = "movie-events"
)

func main() {
	kafkaBroker = os.Getenv("KAFKA_BROKERS")
	if kafkaBroker == "" {
		kafkaBroker = "localhost:9092"
	}

	// Запускаем консьюмеры в фоне
	go consumeMessages(userTopic)
	go consumeMessages(paymentTopic)
	go consumeMessages(movieTopic)

	http.HandleFunc("/api/events/health", healthHandler)

	http.HandleFunc("/api/events/user", userEventHandler)
	http.HandleFunc("/api/events/payment", paymentEventHandler)
	http.HandleFunc("/api/events/movie", movieEventHandler)

	port := "8082"
	log.Printf("Events service started on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]bool{"status": true}
	json.NewEncoder(w).Encode(resp)
}

func userEventHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var event UserEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		httpError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Проверка обязательных полей
	if event.UserID == 0 || event.Action == "" || event.Timestamp == "" {
		httpError(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	sendEvent(w, userTopic, event)
}

func paymentEventHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var event PaymentEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		httpError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if event.PaymentID == 0 || event.UserID == 0 || event.Amount == 0 || event.Status == "" || event.Timestamp == "" {
		httpError(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	sendEvent(w, paymentTopic, event)
}

func movieEventHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var event MovieEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		httpError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if event.MovieID == 0 || event.Title == "" || event.Action == "" {
		httpError(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	sendEvent(w, movieTopic, event)
}

func sendEvent(w http.ResponseWriter, topic string, event interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	writer := kafka.NewWriter(kafka.WriterConfig{
		Brokers:  []string{kafkaBroker},
		Topic:    topic,
		Balancer: &kafka.Hash{},
	})
	defer writer.Close()

	// Сериализуем событие в JSON
	data, err := json.Marshal(event)
	if err != nil {
		httpError(w, "Failed to marshal event: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Создаём сообщение Kafka
	msg := kafka.Message{
		Key:   []byte(fmt.Sprintf("%d", time.Now().UnixNano())), // ключ - timestamp
		Value: data,
	}

	// Публикуем сообщение
	err = writer.WriteMessages(ctx, msg)
	if err != nil {
		httpError(w, "Failed to write message to Kafka: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// В ответ возвращаем статус и данные
	resp := EventResponse{
		Status:    "success",
		Partition: 0,       // Kafka-go не возвращает partition и offset напрямую при WriteMessages
		Offset:    0,       // Для MVP можно вернуть 0 или -1
		Event:     event,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func httpError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}

// Функция консьюмера, которая читает сообщения из топика и логирует их
func consumeMessages(topic string) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{kafkaBroker},
		GroupID: "events-service-group",
		Topic:   topic,
		MinBytes: 1,
		MaxBytes: 10e6,
	})

	defer r.Close()

	log.Printf("Kafka consumer started for topic: %s", topic)

	for {
		m, err := r.ReadMessage(context.Background())
		if err != nil {
			log.Printf("Error reading message from topic %s: %v", topic, err)
			time.Sleep(time.Second)
			continue
		}

		log.Printf("Consumed message from topic %s: key=%s value=%s partition=%d offset=%d",
			topic, string(m.Key), string(m.Value), m.Partition, m.Offset)
	}
}