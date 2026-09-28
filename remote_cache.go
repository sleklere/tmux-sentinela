package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// All sidebars on one tmux server share a single remote poller. The first
// sidebar writes snapshots into server options; the others only read them.
// Names are hashed so arbitrary SSH aliases never become tmux option names.
type cachedRemote struct {
	Host   string    `json:"host"`
	Agents []Agent   `json:"agents"`
	Error  string    `json:"error,omitempty"`
	At     time.Time `json:"at"`
}

func remoteCacheOption(host string) string {
	sum := sha256.Sum256([]byte(host))
	return "@sentinela_poll_" + hex.EncodeToString(sum[:8])
}

func saveRemote(result remoteResult) {
	entry := cachedRemote{Host: result.host, Agents: result.agents, At: result.at}
	if result.err != nil {
		entry.Error = result.err.Error()
	}
	encoded, err := json.Marshal(entry)
	if err == nil {
		tmux("set-option", "-g", remoteCacheOption(result.host), string(encoded))
	}
}

func loadRemote(host string) (remoteResult, bool) {
	encoded, err := tmux("show-option", "-gqv", remoteCacheOption(host))
	if err != nil || encoded == "" {
		return remoteResult{}, false
	}
	var entry cachedRemote
	if json.Unmarshal([]byte(encoded), &entry) != nil || entry.Host != host || entry.At.IsZero() {
		return remoteResult{}, false
	}
	result := remoteResult{host: host, agents: entry.Agents, at: entry.At}
	if entry.Error != "" {
		result.err = errors.New(entry.Error)
	} else if time.Since(entry.At) > 10*time.Second {
		result.err = errors.New("unavailable")
	}
	return result, true
}
