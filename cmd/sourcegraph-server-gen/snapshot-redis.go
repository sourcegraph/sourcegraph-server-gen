package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/garyburd/redigo/redis"
)

var localPort = "6380"

const redisKeysPattern = "user_activity*"

func init() {
	if localPortEnv := os.Getenv("LOCAL_REDIS_PORT"); localPortEnv != "" {
		localPort = localPortEnv
	}
}

var pool = &redis.Pool{
	MaxIdle:     3,
	IdleTimeout: 240 * time.Second,
	Dial: func() (redis.Conn, error) {
		c, err := redis.Dial("tcp", fmt.Sprintf(":%s", localPort))
		if err != nil {
			return nil, err
		}
		return c, err
	},
	TestOnBorrow: func(c redis.Conn, t time.Time) error {
		_, err := c.Do("PING")
		return err
	},
}

func createRedisSnapshot(outDir string) {
	fmt.Fprintln(os.Stderr, "Creating Redis snapshot...")

	portfwd := redisPortForward()
	defer portfwd.Process.Kill()

	c := pool.Get()
	defer c.Close()

	v, err := redis.Values(c.Do("KEYS", redisKeysPattern))
	if err != nil {
		panic(err)
	}
	var keys []string
	if err := redis.ScanSlice(v, &keys); err != nil {
		panic(err)
	}

	keyValues := map[string][]byte{}
	for _, key := range keys {
		if err := c.Send("DUMP", key); err != nil {
			panic(err)
		}
	}
	if err := c.Flush(); err != nil {
		panic(err)
	}
	for _, key := range keys {
		v, err := c.Receive()
		if err != nil {
			panic(err)
		}
		keyValues[key] = v.([]byte)
	}

	kvBytes, err := json.Marshal(keyValues)
	if err != nil {
		panic(err)
	}

	if err := ioutil.WriteFile(filepath.Join(outDir, "redis-store.json"), kvBytes, 0644); err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, "Created Redis snapshot.")
}

func restoreRedisSnapshot(snapDir string) {
	fmt.Fprintf(os.Stderr, "Restoring Redis\n")

	// TODO: check redis version before attempting restore

	fmt.Fprintln(os.Stderr, "Setting up port-forwarding to Redis pod...")
	portfwd := redisPortForward()
	defer portfwd.Process.Kill()
	fmt.Fprintln(os.Stderr, "Set up port-forwarding to Redis pod.")

	f, err := os.Open(filepath.Join(snapDir, "redis-store.json"))
	if err != nil {
		panic(err)
	}
	defer f.Close()

	var keyValues map[string][]byte
	if err := json.NewDecoder(f).Decode(&keyValues); err != nil {
		panic(err)
	}

	c := pool.Get()
	defer c.Close()

	i := 1
	for key, val := range keyValues {
		if err := c.Send("RESTORE", key, 0, val, "REPLACE"); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: RESTORE of key (%d/%d) %q failed: %s\n", i, len(keyValues), key, err)
		} else {
			fmt.Fprintf(os.Stderr, "Restored key (%d/%d) %q\n", i, len(keyValues), key)
		}
		i++
	}
	if _, err := c.Do(""); err != nil {
		panic(fmt.Sprintf("Redis restore failed: %v", err))
	}
}

func redisPortForward() *exec.Cmd {
	server, err := net.Listen("tcp", ":"+localPort)
	if err != nil {
		panic(fmt.Sprintf("Could not bind to port %s for Redis (is something else already listening on it?)", localPort))
	}
	if err := server.Close(); err != nil {
		panic(err)
	}

	redisStorePod := execStr(`kubectl get pods -l app=redis-store -o jsonpath={.items[0].metadata.name}`)
	portfwd := exec.Command("kubectl", "port-forward", redisStorePod, fmt.Sprintf("%s:6379", localPort))
	portfwd.Start()
	time.Sleep(10000 * time.Millisecond)
	return portfwd
}
