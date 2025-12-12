package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Структуры остаются без изменений
type SlangEntry struct {
	Word     string   `json:"word"`
	Meaning  string   `json:"meaning"`
	Example  string   `json:"example"`
	Origin   string   `json:"origin,omitempty"`
	Synonyms []string `json:"synonyms,omitempty"`
}

type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type SlangData struct {
	User     User         `json:"user"`
	Version  string       `json:"version"`
	Entries  []SlangEntry `json:"entries"`
}

const (
	dataFile = "slang.json"
)

// Глобальный мьютекс для безопасного доступа к данным из нескольких горутин
var mu sync.RWMutex

// Загрузка и сохранение остаются почти без изменений
func loadSlangData() SlangData {
	mu.RLock()
	defer mu.RUnlock()

	var slangData SlangData
	if _, err := os.Stat(dataFile); os.IsNotExist(err) {
		return SlangData{Version: "1.0", Entries: []SlangEntry{}, User: User{}}
	}
	data, err := os.ReadFile(dataFile)
	if err != nil {
		fmt.Println("Ошибка чтения файла:", err)
		return SlangData{Version: "1.0", Entries: []SlangEntry{}, User: User{}}
	}
	err = json.Unmarshal(data, &slangData)
	if err != nil {
		fmt.Println("Ошибка парсинга JSON:", err)
		return SlangData{Version: "1.0", Entries: []SlangEntry{}, User: User{}}
	}
	return slangData
}

func saveSlangData(slangData SlangData) {
	mu.Lock()
	defer mu.Unlock()

	data, err := json.MarshalIndent(slangData, "", "  ")
	if err != nil {
		fmt.Println("Ошибка при сериализации:", err)
		return
	}
	if err := os.WriteFile(dataFile, data, 0644); err != nil {
		fmt.Println("Ошибка записи файла:", err)
	}
}

// ————————————————————————
//         HTTP API
// ————————————————————————

// Вспомогательная функция для отправки JSON-ответа
func respondJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

// Вспомогательная функция для чтения JSON из тела запроса
func readJSON(r *http.Request, dst interface{}) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

// GET /api/entries
func handleGetEntries(w http.ResponseWriter, r *http.Request) {
	slangData := loadSlangData()
	respondJSON(w, http.StatusOK, slangData.Entries)
}

// POST /api/entries
func handleAddEntry(w http.ResponseWriter, r *http.Request) {
	var entry SlangEntry
	if err := readJSON(r, &entry); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(entry.Word) == "" || strings.TrimSpace(entry.Meaning) == "" {
		http.Error(w, "Слово и значение обязательны", http.StatusBadRequest)
		return
	}

	slangData := loadSlangData()

	// Проверка дубликата
	for _, e := range slangData.Entries {
		if strings.EqualFold(e.Word, entry.Word) {
			http.Error(w, "Слово уже существует", http.StatusConflict)
			return
		}
	}

	slangData.Entries = append(slangData.Entries, entry)
	saveSlangData(slangData)
	respondJSON(w, http.StatusCreated, map[string]string{"message": "Слово добавлено"})
}

// DELETE /api/entries/{index}
func handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	indexStr := strings.TrimPrefix(r.URL.Path, "/api/entries/")
	index, err := strconv.Atoi(indexStr)
	if err != nil || index < 1 {
		http.Error(w, "Неверный индекс", http.StatusBadRequest)
		return
	}

	slangData := loadSlangData()
	if index > len(slangData.Entries) {
		http.Error(w, "Слово не найдено", http.StatusNotFound)
		return
	}

	slangData.Entries = append(slangData.Entries[:index-1], slangData.Entries[index:]...)
	saveSlangData(slangData)
	respondJSON(w, http.StatusOK, map[string]string{"message": "Слово удалено"})
}

// GET /api/user
func handleGetUser(w http.ResponseWriter, r *http.Request) {
	slangData := loadSlangData()
	if slangData.User.Username == "" {
		http.Error(w, "Пользователь не зарегистрирован", http.StatusUnauthorized)
		return
	}
	// Не возвращаем пароль!
	respondJSON(w, http.StatusOK, map[string]string{"username": slangData.User.Username})
}

// POST /api/register
func handleRegister(w http.ResponseWriter, r *http.Request) {
	type Req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	var req Req
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	if req.Username == "" || len(req.Password) < 4 {
		http.Error(w, "Логин не может быть пустым, пароль — минимум 4 символа", http.StatusBadRequest)
		return
	}

	slangData := loadSlangData()
	if slangData.User.Username != "" {
		http.Error(w, "Пользователь уже зарегистрирован", http.StatusConflict)
		return
	}

	slangData.User = User{Username: req.Username, Password: req.Password}
	saveSlangData(slangData)
	respondJSON(w, http.StatusCreated, map[string]string{"message": "Регистрация успешна"})
}

// POST /api/login
func handleLogin(w http.ResponseWriter, r *http.Request) {
	type Req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	var req Req
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	slangData := loadSlangData()
	if slangData.User.Username == "" {
		http.Error(w, "Сначала зарегистрируйтесь", http.StatusUnauthorized)
		return
	}

	if req.Username == slangData.User.Username && req.Password == slangData.User.Password {
		respondJSON(w, http.StatusOK, map[string]string{
			"message":  "Успешный вход",
			"username": slangData.User.Username,
		})
	} else {
		http.Error(w, "Неверный логин или пароль", http.StatusUnauthorized)
	}
}

// ————————————————————————
//         Запуск API сервера
// ————————————————————————

func startAPIServer() {
	http.HandleFunc("/api/entries", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetEntries(w, r)
		case http.MethodPost:
			handleAddEntry(w, r)
		default:
			http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		}
	})

	// DELETE по пути /api/entries/123
	http.HandleFunc("/api/entries/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			handleDeleteEntry(w, r)
		} else {
			http.Error(w, "Только DELETE разрешён для этого пути", http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/api/user", handleGetUser)
	http.HandleFunc("/api/register", handleRegister)
	http.HandleFunc("/api/login", handleLogin)

	fmt.Println("\n🔧 Запуск API на http://localhost:8080")
	go func() {
		if err := http.ListenAndServe(":8080", nil); err != nil {
			fmt.Printf("❌ Ошибка запуска сервера: %v\n", err)
		}
	}()
}

// ————————————————————————
//         Основная программа
// ————————————————————————

func main() {
	fmt.Println("Словарь современного сленга")
	fmt.Println("---------------------------")

	startAPIServer()

	for {
		fmt.Println("\n=== ГЛАВНОЕ МЕНЮ ===")
		fmt.Println("1. Регистрация")
		fmt.Println("2. Вход")
		fmt.Println("3. Выход")
		fmt.Print("Выберите действие: ")

		var choice string
		fmt.Scanln(&choice)

		switch choice {
		case "1":
			if register() {
				fmt.Println("Регистрация успешна! Теперь войдите в систему.")
			}
		case "2":
			if login() {
				runDictionaryApp()
				return
			}
		case "3":
			fmt.Println("До свидания!")
			// Добавим небольшую паузу, чтобы API успел завершить работу (опционально)
			time.Sleep(100 * time.Millisecond)
			return
		default:
			fmt.Println("Неверный выбор, попробуйте еще раз")
		}
	}
}


func register() bool {
	reader := bufio.NewReader(os.Stdin)
	slangData := loadSlangData()
	if slangData.User.Username != "" {
		fmt.Println("Пользователь уже зарегистрирован. Используйте вход.")
		return false
	}
	fmt.Print("Придумайте логин: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if username == "" {
		fmt.Println("Логин не может быть пустым")
		return false
	}
	fmt.Print("Придумайте пароль: ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)
	if len(password) < 4 {
		fmt.Println("Пароль должен содержать минимум 4 символа")
		return false
	}
	slangData.User = User{Username: username, Password: password}
	saveSlangData(slangData)
	fmt.Printf("Пользователь '%s' успешно зарегистрирован!\n", username)
	return true
}

func login() bool {
	reader := bufio.NewReader(os.Stdin)
	slangData := loadSlangData()
	if slangData.User.Username == "" {
		fmt.Println("Сначала необходимо зарегистрироваться!")
		return false
	}
	for attempts := 3; attempts > 0; attempts-- {
		fmt.Print("Логин: ")
		username, _ := reader.ReadString('\n')
		username = strings.TrimSpace(username)
		fmt.Print("Пароль: ")
		password, _ := reader.ReadString('\n')
		password = strings.TrimSpace(password)
		if username == slangData.User.Username && password == slangData.User.Password {
			fmt.Printf("Добро пожаловать, %s!\n", username)
			fmt.Printf("Загружено слов: %d\n", len(slangData.Entries))
			return true
		}
		if attempts > 1 {
			fmt.Printf("Неверный логин или пароль. Осталось попыток: %d\n", attempts-1)
		} else {
			fmt.Println("Неверный логин или пароль. Попробуйте начать с главного меню.")
		}
	}
	return false
}

func runDictionaryApp() {
	slangData := loadSlangData()
	for {
		fmt.Println("")
		fmt.Println("Что будем делать?")
		fmt.Println("1. Посмотреть все слова")
		fmt.Println("2. Добавить новое слово")
		fmt.Println("3. Удалить слово")
		fmt.Println("4. Выйти из приложения")
		fmt.Print("Твой выбор: ")

		var choice string
		fmt.Scanln(&choice)

		switch choice {
		case "1":
			showAllEntries(slangData)
		case "2":
			addNewEntry(&slangData)
		case "3":
			deleteEntry(&slangData)
		case "4":
			fmt.Println("До свидания!")
			return
		default:
			fmt.Println("Такого варианта нет, попробуй еще раз")
		}
	}
}

func showAllEntries(slangData SlangData) {
	if len(slangData.Entries) == 0 {
		fmt.Println("В словаре пока ничего нет")
		return
	}
	fmt.Printf("\nВсего слов: %d\n", len(slangData.Entries))
	fmt.Println("==========================================")
	for i, entry := range slangData.Entries {
		fmt.Printf("%d. Слово: %s\n", i+1, entry.Word)
		fmt.Printf("   Значение: %s\n", entry.Meaning)
		fmt.Printf("   Пример: %s\n", entry.Example)
		if entry.Origin != "" {
			fmt.Printf("   Откуда: %s\n", entry.Origin)
		}
		if len(entry.Synonyms) > 0 {
			fmt.Printf("   Похожие слова: %s\n", strings.Join(entry.Synonyms, ", "))
		}
		fmt.Println("------------------------------------------")
	}
}

func addNewEntry(slangData *SlangData) {
	reader := bufio.NewReader(os.Stdin)
	var entry SlangEntry
	fmt.Println("\nДобавляем новое слово")
	fmt.Print("Какое слово? ")
	word, _ := reader.ReadString('\n')
	entry.Word = strings.TrimSpace(word)
	for _, e := range slangData.Entries {
		if strings.EqualFold(e.Word, entry.Word) {
			fmt.Printf("Слово '%s' уже есть в словаре\n", entry.Word)
			return
		}
	}
	fmt.Print("Что оно означает? ")
	meaning, _ := reader.ReadString('\n')
	entry.Meaning = strings.TrimSpace(meaning)
	fmt.Print("Приведи пример использования: ")
	example, _ := reader.ReadString('\n')
	entry.Example = strings.TrimSpace(example)
	fmt.Print("Откуда оно произошло (можно пропустить)? ")
	origin, _ := reader.ReadString('\n')
	entry.Origin = strings.TrimSpace(origin)
	fmt.Print("Какие есть похожие слова (через запятую, можно пропустить)? ")
	synonyms, _ := reader.ReadString('\n')
	synonyms = strings.TrimSpace(synonyms)
	if synonyms != "" {
		entry.Synonyms = strings.Split(synonyms, ",")
		for i := range entry.Synonyms {
			entry.Synonyms[i] = strings.TrimSpace(entry.Synonyms[i])
		}
	}
	slangData.Entries = append(slangData.Entries, entry)
	saveSlangData(*slangData)
	fmt.Printf("Отлично! Слово '%s' добавлено в словарь\n", entry.Word)
}

func deleteEntry(slangData *SlangData) {
	if len(slangData.Entries) == 0 {
		fmt.Println("В словаре ничего нет, удалять нечего")
		return
	}
	showAllEntries(*slangData)
	var index int
	fmt.Print("\nКакое слово удаляем (введи номер)? ")
	_, err := fmt.Scanln(&index)
	if err != nil || index < 1 || index > len(slangData.Entries) {
		fmt.Println("Нет такого номера")
		return
	}
	wordToDelete := slangData.Entries[index-1].Word
	fmt.Printf("Точно удалить '%s'? (да/нет): ", wordToDelete)
	var confirm string
	fmt.Scanln(&confirm)
	if strings.ToLower(confirm) == "да" || strings.ToLower(confirm) == "д" || strings.ToLower(confirm) == "y" {
		slangData.Entries = append(slangData.Entries[:index-1], slangData.Entries[index:]...)
		saveSlangData(*slangData)
		fmt.Printf("Слово '%s' удалено\n", wordToDelete)
	} else {
		fmt.Println("Удаление отменено")
	}
}