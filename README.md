# 🚀 Real-time Collaborative Task Board (Go & WebSocket)

Proyek ini adalah implementasi sederhana dari papan tugas (*Task Board*) kolaboratif yang diperbarui secara *real-time* menggunakan **Golang** sebagai *backend* dan **WebSocket** untuk sinkronisasi data antar klien. Data tugas disimpan sementara di memori server.

## 🎯 Permasalahan yang Diselesaikan

Proyek ini mengatasi masalah **sinkronisasi data mendesak** dalam tim *remote*. Ketika satu anggota tim menambahkan, menghapus, atau menandai tugas sebagai selesai, pembaruan tersebut akan **langsung** terlihat di layar semua anggota tim lainnya tanpa perlu *refresh*. Ini sangat ideal untuk *action item* rapat atau *bug* kritis.

## 💻 Struktur Proyek
real-time-task-board/ ├── main.go # Server Golang (WebSocket & State Logic) ├── go.mod # Dependency Golang └── static/ └── index.html # Frontend (HTML, CSS, JavaScript)

## 🛠️ Cara Menjalankan

### Persyaratan

* Go (Golang) 1.16+
* Koneksi internet (untuk mengunduh *dependency* pertama kali)

### Langkah-langkah

1.  **Inisialisasi Modul & Dependency:**
    ```bash
    go mod init simple-task-board
    go get [github.com/gorilla/websocket](https://github.com/gorilla/websocket) [github.com/google/uuid](https://github.com/google/uuid)
    ```

2.  **Jalankan Server:**
    ```bash
    go run main.go
    ```
    Server akan berjalan di `http://localhost:8080`.

3.  **Akses Aplikasi:**
    Buka `http://localhost:8080` di beberapa *browser* atau tab untuk menguji fitur kolaborasi *real-time*.

## 🔑 Penjelasan Kode Singkat

### A. Backend Golang (`main.go`)

Kode Go berpusat pada *design pattern* **Hub** dan penggunaan **Goroutine** untuk menangani konkurensi.

| Komponen (Struct/Fungsi) | Peran | Konsep Go Utama |
| :--- | :--- | :--- |
| **`Task`** *Struct* | Model data untuk satu tugas (ID, Text, Status). | - |
| **`StateHub`** *Struct* | **Pusat Logika Aplikasi**. Mengelola daftar klien (`clients`) dan status tugas (`tasks` map). | `sync.RWMutex` (untuk *thread safety* pada data `tasks`) |
| **`hub.run()`** | Berjalan di Goroutine terpisah. Bertanggung jawab memproses *channel* (`register`, `unregister`, `broadcast`). | **Goroutine, Channel, `select` statement** |
| **`serveWs()`** | Meng-*upgrade* koneksi HTTP klien menjadi koneksi **WebSocket**. | `websocket.Upgrader.Upgrade()` |
| **`readPump()`** | Goroutine per klien baru. Tugasnya hanya membaca pesan masuk dari klien dan meneruskannya ke `hub.handleMessage`. | **Goroutine** (untuk mencegah satu klien memblokir klien lain) |
| **`handleMessage()`** | Menganalisis tipe pesan (e.g., `NEW_TASK`, `UPDATE_STATUS`) dari klien, memproses perubahan data di **memori**, dan memicu `sendStateUpdate`. | JSON Parsing, Logika Bisnis |

### B. Frontend JavaScript (`static/index.html`)

Frontend menggunakan API **Native WebSocket** dan DOM Manipulation untuk mencapai *real-time*:

1.  **Koneksi:** Menginisialisasi koneksi `new WebSocket("ws://localhost:8080/ws")`.
2.  **Pesan Keluar (Egress):** Mengemas aksi pengguna (Tambah Tugas, Hapus, Update Status) menjadi objek JSON dan mengirimkannya melalui `socket.send(JSON.stringify(msg))`.
3.  **Pesan Masuk (Ingress):** Fungsi `socket.onmessage` menerima *event*. Klien menganalisis properti `type` dari JSON yang diterima (`TASK_ADDED`, `TASK_DELETED`, `STATUS_UPDATED`).
4.  **Perbaruan DOM Selektif:** Berdasarkan tipe pesan, JavaScript hanya memanipulasi elemen DOM yang terpengaruh (misalnya, hanya menambah elemen `<li>` baru untuk `TASK_ADDED` atau mengubah kelas CSS untuk `STATUS_UPDATED`). Ini membuat tampilan sangat responsif dan efisien.

---

## 🛑 Catatan Penting (Basis Memori)

Karena proyek ini berjalan tanpa *database* untuk mencapai kesederhanaan, **SEMUA data tugas akan hilang** setiap kali server Golang dimatikan. Untuk lingkungan produksi, bagian logika penyimpanan data (di `main.go`) harus diganti dengan panggilan ke PostgreSQL, MySQL, atau Redis.