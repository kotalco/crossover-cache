# RESP
resp: Go Redis Client with Redis Serialization Protocol (RESP) Support
A lightweight Redis client package that implements the Redis Serialization Protocol (RESP) for communication with Redis servers. 
This package is designed to be minimalistic, with no external dependencies, relying solely on TCP networking and Redis serialization protocol (RESP),
It provides basic methods for caching and rate-limiting operations.

## Features
- No external dependencies
- Basic caching operations
- Basic rate-limiting functionality
- Direct TCP connections to Redis server
- Custom implementation of the Redis Serialization Protocol (RESP)


## Installation
`go get github.com/kotalco/resp`

## Usage
see usage in the example folder