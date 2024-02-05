package crossover_cache

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"github.com/kotalco/resp"
	"log"
	"net/http"
)

const (
	DefaultRedisPoolSize = 10
	DefaultCacheExpiry   = 15
)

type CachedResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

type Config struct {
	RedisAddress  string
	RedisAuth     string
	RedisPoolSize int
	CacheExpiry   int //in seconds
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{}
}

type Cache struct {
	next          http.Handler
	name          string
	resp          resp.IClient
	redisAuth     string
	redisAddress  string
	redisPoolSize int
	cacheExpiry   int
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.RedisPoolSize == 0 {
		config.RedisPoolSize = DefaultRedisPoolSize
	}
	if config.CacheExpiry == 0 {
		config.CacheExpiry = DefaultCacheExpiry
	}
	gob.Register(CachedResponse{})

	respClient, err := resp.NewRedisClient(config.RedisAddress, config.RedisPoolSize, config.RedisAuth)
	if err != nil {
		return nil, fmt.Errorf("can't create redis client")
	}

	handler := &Cache{
		next:          next,
		name:          name,
		resp:          respClient,
		redisAddress:  config.RedisAddress,
		redisAuth:     config.RedisAuth,
		redisPoolSize: config.RedisPoolSize,
		cacheExpiry:   config.CacheExpiry,
	}
	return handler, nil
}

func (c *Cache) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// cache key based on the request
	cacheKey := req.URL.Path

	// retrieve the cached response
	cachedData, err := c.resp.Get(req.Context(), cacheKey)
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
		}
		log.Printf("Failed to serialize response for caching: %s", err.Error())
		_ = c.resp.Delete(req.Context(), cacheKey)
	}

	// Cache miss - record the response
	recorder := &responseRecorder{rw: rw}
	c.next.ServeHTTP(recorder, req)

	// Serialize the response data
	cachedResponse := CachedResponse{
		StatusCode: recorder.status,
		Headers:    recorder.Header().Clone(), // Convert http.Header to a map for serialization
		Body:       recorder.body.Bytes(),
	}
	var buffer bytes.Buffer
	enc := gob.NewEncoder(&buffer)
	if err := enc.Encode(cachedResponse); err != nil {
		log.Printf("Failed to serialize response for caching: %s", err)
	}

	// Store the serialized response in Redis as a string with an expiration time
	if err := c.resp.SetWithTTL(req.Context(), cacheKey, buffer.String(), c.cacheExpiry); err != nil {
		log.Println("Failed to cache response in Redis:", err)
	}

	// Write the original response
	rw.WriteHeader(recorder.status)
	_, _ = rw.Write(recorder.body.Bytes())
}
