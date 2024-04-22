package crossover_cache

import (
	"bytes"
	"context"
	"encoding/base64"
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
		log.Println("cache hit")
		// Cache hit - decode the base64 string
		data, err := base64.StdEncoding.DecodeString(cachedData)
		if err != nil {
			log.Printf("Failed to decode base64 string: %s", err)
			return
		}

		// Parse the cached response and write it to the original ResponseWriter
		var cachedResponse CachedResponse
		buffer := bytes.NewBuffer(data) // Use the decoded byte slice
		dec := gob.NewDecoder(buffer)
		if err := dec.Decode(&cachedResponse); err == nil {
			for key, values := range cachedResponse.Headers {
				for _, value := range values {
					rw.Header().Add(key, value)
				}
			}
			log.Println("writing response from cache")
			rw.WriteHeader(cachedResponse.StatusCode)
			_, _ = rw.Write(cachedResponse.Body)
			return
		} else {
			log.Printf("Failed to deserialize response from cache: %s", err.Error())
			_ = respClient.Delete(req.Context(), cacheKey)
		}
	}

	log.Println("cache hit")
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
	} else {
		log.Println("caching response to redis")
		// Encode the buffer to a base64 string
		encodedString := base64.StdEncoding.EncodeToString(buffer.Bytes())
		// Store the serialized response in Redis
		if err := respClient.SetWithTTL(req.Context(), cacheKey, encodedString, c.cacheExpiry); err != nil {
			log.Printf("Failed to cache response in Redis: %s", err.Error())
		}
	}

	// Write the response to the client
	for key, values := range cachedResponse.Headers {
		for _, value := range values {
			rw.Header().Add(key, value)
		}
	}
	log.Println("writing response")
	rw.WriteHeader(cachedResponse.StatusCode)
	_, err = rw.Write(cachedResponse.Body)
	return

}
