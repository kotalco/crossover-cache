package crossover_cache

import (
	"bytes"
	"context"
	"encoding/gob"
	"github.com/kotalco/resp"
	"log"
	"net/http"
)

const (
	DefaultCacheExpiry = 15
)

type CachedResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

type Config struct {
	RedisAddress string
	RedisAuth    string
	CacheExpiry  int //in seconds
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{}
}

type Cache struct {
	next         http.Handler
	name         string
	redisAuth    string
	redisAddress string
	cacheExpiry  int
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.CacheExpiry == 0 {
		config.CacheExpiry = DefaultCacheExpiry
	}
	gob.Register(CachedResponse{})

	handler := &Cache{
		next:         next,
		name:         name,
		redisAddress: config.RedisAddress,
		redisAuth:    config.RedisAuth,
		cacheExpiry:  config.CacheExpiry,
	}
	return handler, nil
}

func (c *Cache) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	respClient, err := resp.NewRedisClient(c.redisAddress, c.redisAuth)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		log.Printf("Failed to create Redis Connection %s", err.Error())
		rw.Write([]byte("something went wrong"))
		return
	}
	defer respClient.Close()
	// cache key based on the request
	cacheKey := req.URL.Path

	// retrieve the cached response
	cachedData, err := respClient.Get(req.Context(), cacheKey)
	if err == nil && cachedData != "" {
		// Cache hit - parse the cached response and write it to the original ResponseWriter
		var cachedResponse CachedResponse
		buffer := bytes.NewBufferString(cachedData)
		dec := gob.NewDecoder(buffer)
		if err := dec.Decode(&cachedResponse); err == nil {
			for key, values := range cachedResponse.Headers {
				for _, value := range values {
					rw.Header().Add(key, value)
				}
			}
			rw.WriteHeader(cachedResponse.StatusCode)
			_, _ = rw.Write(cachedResponse.Body)
			return
		} else {
			log.Printf("Failed to serialize response for caching: %s", err.Error())
			_ = respClient.Delete(req.Context(), cacheKey)
		}

	}

	// Cache miss - record the response
	recorder := &responseRecorder{
		rw:     rw,
		header: rw.Header().Clone(), // Initialize with the original headers.
	}
	c.next.ServeHTTP(recorder, req)

	// Serialize the response data
	cachedResponse := CachedResponse{
		StatusCode: recorder.status,
		Headers:    recorder.Header(), // Convert http.Header to a map for serialization
		Body:       recorder.body.Bytes(),
	}
	var buffer bytes.Buffer
	enc := gob.NewEncoder(&buffer)
	if err := enc.Encode(cachedResponse); err != nil {
		log.Printf("Failed to serialize response for caching: %s", err)
		http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
		return
	} else {
		// Store the serialized response in Redis
		if err := respClient.SetWithTTL(req.Context(), cacheKey, buffer.String(), c.cacheExpiry); err != nil {
			log.Printf("Failed to cache response in Redis: %s", err.Error())
		}
	}

	if _, err := rw.Write(recorder.body.Bytes()); err != nil {
		log.Printf("Failed to write response body: %s", err)
		return
	}
	return
}
