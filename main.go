package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// --- STRUCT DATA ---
type Question struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Votes int    `json:"votes"`
	Time  string `json:"time"`
}

type Message struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"` // Menggunakan RawMessage untuk parsing dinamis
}

// --- HUB DAN STATE MANAGEMENT ---

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Izinkan semua Origin untuk pengembangan
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// StateHub mengelola koneksi dan status papan Q&A
type StateHub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn

	// Logika Bisnis: Menyimpan pertanyaan
	questions map[string]*Question
	mu        sync.RWMutex // Mutex untuk melindungi map questions
}

func newHub() *StateHub {
	return &StateHub{
		broadcast:  make(chan []byte),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		clients:    make(map[*websocket.Conn]bool),
		questions:  make(map[string]*Question),
	}
}

// run mengelola channel dan state
func (h *StateHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("Client connected. Total clients: %d", len(h.clients))
			// Kirim state awal ke klien baru
			h.sendInitialState(client)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close()
			}
			h.mu.Unlock()
			log.Printf("Client disconnected. Total clients: %d", len(h.clients))

		case message := <-h.broadcast:
			// Siarkan pesan ke semua klien
			h.mu.RLock()
			for client := range h.clients {
				err := client.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					log.Printf("Error writing message: %v", err)
					client.Close()
					h.unregister <- client // unregister via channel
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Helper untuk mendapatkan list pertanyaan yang sudah diurutkan (berdasarkan votes)
func (h *StateHub) getSortedQuestions() []*Question {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var sorted []*Question
	for _, q := range h.questions {
		sorted = append(sorted, q)
	}

	// Urutkan berdasarkan Votes (descending)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Votes > sorted[j].Votes
	})
	return sorted
}

// Kirim state saat ini ke klien baru
func (h *StateHub) sendInitialState(conn *websocket.Conn) {
	sortedQs := h.getSortedQuestions()

	initMsg := struct {
		Type string      `json:"type"`
		Data []*Question `json:"data"`
	}{
		Type: "INIT_STATE",
		Data: sortedQs,
	}

	response, _ := json.Marshal(initMsg)
	conn.WriteMessage(websocket.TextMessage, response)
}

// --- HANDLER LOGIC ---

// handleMessage memproses pesan dari klien
func (h *StateHub) handleMessage(message []byte) {
	var msg Message
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error unmarshalling message: %v", err)
		return
	}

	switch msg.Type {
	case "NEW_QUESTION":
		h.handleNewQuestion(msg.Data)
	case "VOTE":
		h.handleVote(msg.Data)
	default:
		log.Printf("Unknown message type: %s", msg.Type)
	}
}

// Proses pertanyaan baru
func (h *StateHub) handleNewQuestion(data []byte) {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		log.Printf("Error unmarshalling question text: %v", err)
		return
	}

	newQ := &Question{
		ID:    uuid.New().String(),
		Text:  text,
		Votes: 0,
		Time:  time.Now().Format("15:04:05"),
	}

	h.mu.Lock()
	h.questions[newQ.ID] = newQ
	h.mu.Unlock()

	// Kirim state terbaru ke semua klien
	h.sendStateUpdate()
}

// Proses vote
func (h *StateHub) handleVote(data []byte) {
	var qID string
	if err := json.Unmarshal(data, &qID); err != nil {
		log.Printf("Error unmarshalling vote ID: %v", err)
		return
	}

	h.mu.Lock()
	if q, ok := h.questions[qID]; ok {
		q.Votes++
	} else {
		log.Printf("Question ID not found for vote: %s", qID)
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	// Kirim state terbaru ke semua klien
	h.sendStateUpdate()
}

// Kirim state terbaru (seluruh list pertanyaan) ke channel broadcast
func (h *StateHub) sendStateUpdate() {
	sortedQs := h.getSortedQuestions()

	updateMsg := struct {
		Type string      `json:"type"`
		Data []*Question `json:"data"`
	}{
		Type: "UPDATE_STATE",
		Data: sortedQs,
	}

	response, err := json.Marshal(updateMsg)
	if err != nil {
		log.Printf("Error marshalling update state: %v", err)
		return
	}
	h.broadcast <- response
}

// --- SERVER HTTP DAN WS HANDLERS ---

func serveWs(hub *StateHub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	hub.register <- conn

	go readPump(hub, conn)
}

func readPump(hub *StateHub, conn *websocket.Conn) {
	defer func() {
		hub.unregister <- conn
		conn.Close()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		// Proses pesan yang masuk dari klien
		hub.handleMessage(message)
	}
}

func main() {
	hub := newHub()
	go hub.run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	// Sajikan file statis (HTML/JS)
	http.Handle("/", http.FileServer(http.Dir("./static")))

	fmt.Println("Q&A Board Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
