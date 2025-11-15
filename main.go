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

// Task struct mewakili satu tugas
type Task struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status bool   `json:"status"` // false=Pending, true=Completed
	Time   string `json:"time"`
}

// Message struct untuk komunikasi WebSocket
type Message struct {
	Type string          `json:"type"` // "NEW_TASK", "DELETE_TASK", "UPDATE_STATUS", "INIT_STATE"
	Data json.RawMessage `json:"data"`
}

// --- HUB DAN STATE MANAGEMENT (Menggunakan Memori) ---

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// StateHub mengelola koneksi dan status papan Task di memori
type StateHub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn

	// STATE: Tugas disimpan di memori
	tasks map[string]*Task
	mu    sync.RWMutex // Mutex untuk melindungi map tasks
}

func newHub() *StateHub {
	return &StateHub{
		broadcast:  make(chan []byte),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		clients:    make(map[*websocket.Conn]bool),
		tasks:      make(map[string]*Task), // Inisialisasi map tugas
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

// Helper untuk mendapatkan list tugas (diurutkan berdasarkan waktu terbaru)
func (h *StateHub) getTasksSorted() []*Task {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var sorted []*Task
	for _, t := range h.tasks {
		sorted = append(sorted, t)
	}

	// Urutkan berdasarkan waktu (descending - terbaru di atas)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Time > sorted[j].Time // Membandingkan string waktu
	})
	return sorted
}

// Kirim state saat ini ke klien baru
func (h *StateHub) sendInitialState(conn *websocket.Conn) {
	tasks := h.getTasksSorted()

	initMsg := struct {
		Type string  `json:"type"`
		Data []*Task `json:"data"`
	}{
		Type: "INIT_STATE",
		Data: tasks,
	}

	response, _ := json.Marshal(initMsg)
	conn.WriteMessage(websocket.TextMessage, response)
}

func (h *StateHub) sendStateUpdate(taskType string, data interface{}) {
	updateMsg := struct {
		Type string      `json:"type"`
		Data interface{} `json:"data"`
	}{
		Type: taskType,
		Data: data,
	}

	response, err := json.Marshal(updateMsg)
	if err != nil {
		log.Printf("Error marshalling update state: %v", err)
		return
	}
	h.broadcast <- response
}

// --- HANDLER LOGIC BARU ---

func (h *StateHub) handleMessage(message []byte) {
	var msg Message
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error unmarshalling message: %v", err)
		return
	}

	switch msg.Type {
	case "NEW_TASK":
		h.handleNewTask(msg.Data)
	case "DELETE_TASK":
		h.handleDeleteTask(msg.Data)
	case "UPDATE_STATUS":
		h.handleUpdateStatus(msg.Data)
	default:
		log.Printf("Unknown message type: %s", msg.Type)
	}
}

// Proses tugas baru
func (h *StateHub) handleNewTask(data []byte) {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		log.Printf("Error unmarshalling task text: %v", err)
		return
	}

	newTask := &Task{
		ID:     uuid.New().String(),
		Text:   text,
		Status: false,
		// Format waktu yang dapat diurutkan (ISO 8601)
		Time: time.Now().Format(time.RFC3339),
	}

	// Simpan ke memori
	h.mu.Lock()
	h.tasks[newTask.ID] = newTask
	h.mu.Unlock()

	// Kirim tugas yang baru ditambahkan ke semua klien
	h.sendStateUpdate("TASK_ADDED", newTask)
}

// Proses hapus tugas
func (h *StateHub) handleDeleteTask(data []byte) {
	var taskID string
	if err := json.Unmarshal(data, &taskID); err != nil {
		log.Printf("Error unmarshalling task ID: %v", err)
		return
	}

	// Hapus dari memori
	h.mu.Lock()
	delete(h.tasks, taskID)
	h.mu.Unlock()

	// Kirim ID tugas yang dihapus ke semua klien
	h.sendStateUpdate("TASK_DELETED", taskID)
}

// Struct untuk update status
type StatusUpdate struct {
	ID     string `json:"id"`
	Status bool   `json:"status"`
}

// Proses update status tugas
func (h *StateHub) handleUpdateStatus(data []byte) {
	var update StatusUpdate
	if err := json.Unmarshal(data, &update); err != nil {
		log.Printf("Error unmarshalling status update: %v", err)
		return
	}

	// Update di memori
	h.mu.Lock()
	if t, ok := h.tasks[update.ID]; ok {
		t.Status = update.Status
	} else {
		log.Printf("Task ID not found for status update: %s", update.ID)
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	// Kirim pembaruan status ke semua klien
	h.sendStateUpdate("STATUS_UPDATED", update)
}

// --- MAIN FUNCTION dan WS Handlers ---

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
		// Kita hanya peduli dengan menerima pesan teks
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		hub.handleMessage(message)
	}
}

func main() {
	hub := newHub()
	go hub.run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	http.Handle("/", http.FileServer(http.Dir("./static")))

	fmt.Println("Simple Collaborative Task Board Server (Memory-based) started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
