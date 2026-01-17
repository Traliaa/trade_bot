package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"trade_bot/internal/models"
)

type User struct {
	path string

	mu     sync.Mutex
	cache  map[int64]*models.UserSettings
	loaded bool
}

const defaultPath = "data/users.json"

func NewUser() *User {
	path := os.Getenv("BOT_STORE_PATH")
	if path == "" {
		path = defaultPath
	}
	return &User{
		path:  path,
		cache: make(map[int64]*models.UserSettings),
	}
}

// ---- public API (как у pg.User) ----

func (u *User) Create(ctx context.Context, user *models.UserSettings) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if err := u.loadLocked(); err != nil {
		return err
	}
	u.cache[user.UserID] = cloneUser(user)
	return u.saveLocked()
}

func (u *User) Update(ctx context.Context, user *models.UserSettings) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if err := u.loadLocked(); err != nil {
		return err
	}
	if _, ok := u.cache[user.UserID]; !ok {
		// как в PG: можно либо ошибку, либо upsert. Сделаем upsert — быстрее для продакшена.
	}
	u.cache[user.UserID] = cloneUser(user)
	return u.saveLocked()
}

func (u *User) Get(ctx context.Context, userID int64) (*models.UserSettings, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if err := u.loadLocked(); err != nil {
		return nil, err
	}
	v, ok := u.cache[userID]
	if !ok {
		return nil, nil // или ошибку "not found" — как у тебя принято
	}
	return cloneUser(v), nil
}

func (u *User) Delete(ctx context.Context, user *models.UserSettings) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if err := u.loadLocked(); err != nil {
		return err
	}
	delete(u.cache, user.UserID)
	return u.saveLocked()
}

func (u *User) List(ctx context.Context) ([]*models.UserSettings, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if err := u.loadLocked(); err != nil {
		return nil, err
	}
	out := make([]*models.UserSettings, 0, len(u.cache))
	for _, v := range u.cache {
		out = append(out, cloneUser(v))
	}
	return out, nil
}

// ---- storage format ----

type snapshot struct {
	UpdatedAt time.Time              `json:"updated_at"`
	Users     []*models.UserSettings `json:"users"`
}

func (u *User) loadLocked() error {
	if u.loaded {
		return nil
	}

	b, err := os.ReadFile(u.path)
	if err != nil {
		if os.IsNotExist(err) {
			u.loaded = true
			return nil
		}
		return fmt.Errorf("read %s: %w", u.path, err)
	}

	var snap snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return fmt.Errorf("decode %s: %w", u.path, err)
	}

	u.cache = make(map[int64]*models.UserSettings, len(snap.Users))
	for _, us := range snap.Users {
		if us == nil {
			continue
		}
		u.cache[us.UserID] = cloneUser(us)
	}

	u.loaded = true
	return nil
}

func (u *User) saveLocked() error {
	dir := filepath.Dir(u.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	users := make([]*models.UserSettings, 0, len(u.cache))
	for _, v := range u.cache {
		users = append(users, cloneUser(v))
	}

	snap := snapshot{
		UpdatedAt: time.Now(),
		Users:     users,
	}

	b, err := json.MarshalIndent(&snap, "", "  ")
	if err != nil {
		return err
	}

	tmp := u.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, u.path) // атомарно
}

// clone чтобы никто извне не мутировал shared ptr
func cloneUser(in *models.UserSettings) *models.UserSettings {
	if in == nil {
		return nil
	}
	b, _ := json.Marshal(in)
	var out models.UserSettings
	_ = json.Unmarshal(b, &out)
	return &out
}
