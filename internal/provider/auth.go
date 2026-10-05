package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"
)

// KeyStore menyimpan API key provider: keychain OS bila tersedia,
// dengan fallback file kredensial terproteksi (0600).
// Variabel lingkungan selalu dicek lebih dulu oleh Resolve.
type KeyStore struct {
	dir string // lokasi credentials.json

	mu    sync.Mutex
	krOK  map[string]bool // hasil percobaan keyring per provider
	files string          // isi credentials.json terakhir (cache)
}

// NewKeyStore membuat store di dir (biasanya config.DataDir()).
func NewKeyStore(dir string) *KeyStore {
	return &KeyStore{dir: dir, krOK: map[string]bool{}}
}

const keyringService = "jenderalcode"
const credFile = "credentials.json"

// Set menyimpan key untuk provider.
func (s *KeyStore) Set(providerID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Coba keychain OS dulu.
	if err := keyring.Set(keyringService, providerID, key); err == nil {
		delete(s.krOK, providerID)
		return nil
	}
	// Fallback: file kredensial.
	creds, err := s.readCreds()
	if err != nil {
		return err
	}
	creds[providerID] = key
	return s.writeCreds(creds)
}

// Get membaca key dari file fallback saja (tanpa env, tanpa keyring).
func (s *KeyStore) Get(providerID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Coba keychain.
	if key, err := keyring.Get(keyringService, providerID); err == nil && key != "" {
		return key, true
	}
	creds, err := s.readCreds()
	if err != nil {
		return "", false
	}
	key, ok := creds[providerID]
	return key, ok && key != ""
}

// Delete menghapus key dari keychain dan file.
func (s *KeyStore) Delete(providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = keyring.Delete(keyringService, providerID)
	creds, err := s.readCreds()
	if err != nil {
		return nil
	}
	delete(creds, providerID)
	return s.writeCreds(creds)
}

// Sources mengembalikan lokasi penyimpanan yang dipakai (untuk pesan UI).
func (s *KeyStore) FileFallback() string {
	return filepath.Join(s.dir, credFile)
}

func (s *KeyStore) readCreds() (map[string]string, error) {
	out := map[string]string{}
	b, err := os.ReadFile(filepath.Join(s.dir, credFile))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("credentials.json rusak: %w", err)
	}
	return out, nil
}

func (s *KeyStore) writeCreds(creds map[string]string) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(s.dir, credFile)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ResolveKey mencari API key provider dengan urutan:
//  1. variabel lingkungan (env_key katalog, mis. OPENAI_API_KEY)
//  2. keychain OS / file kredensial
//  3. config provider.<id>.api_key
func ResolveKey(providerID, envKey, configKey string, keys *KeyStore) (string, bool) {
	if envKey != "" {
		if v := os.Getenv(envKey); v != "" {
			return v, true
		}
	}
	if generic := os.Getenv(envGeneric(providerID)); generic != "" {
		return generic, true
	}
	if keys != nil {
		if v, ok := keys.Get(providerID); ok {
			return v, true
		}
	}
	if configKey != "" {
		return configKey, true
	}
	return "", false
}

// envGeneric menghasilkan nama env generik: kilo -> KILO_API_KEY.
func envGeneric(providerID string) string {
	up := ""
	for _, r := range providerID {
		if r >= 'a' && r <= 'z' {
			up += string(r - 32)
		} else if r >= 'A' && r <= 'Z' {
			up += string(r)
		} else {
			up += "_"
		}
	}
	return up + "_API_KEY"
}
