package redis_bloom_go

import (
	"errors"
	"fmt"
	"github.com/gomodule/redigo/redis"
	"strconv"
	"strings"
)

// TODO: refactor this hard limit and revise client locking
// Client Max Connections
var maxConns = 500

// Client is an interface to time series redis commands
type Client struct {
	Pool ConnPool
	Name string
}

// NewClient creates a new client connecting to the redis host, and using the given name as key prefix.
// Addr can be a single host:port pair, or a comma separated list of host:port,host:port...
// In the case of multiple hosts we create a multi-pool and select connections at random
func NewClient(addr, name string, authPass *string) *Client {
	addrs := strings.Split(addr, ",")
	var pool ConnPool
	if len(addrs) == 1 {
		pool = NewSingleHostPool(addrs[0], authPass)
	} else {
		pool = NewMultiHostPool(addrs, authPass)
	}
	ret := &Client{
		Pool: pool,
		Name: name,
	}
	return ret
}

// NewClientFromPool creates a new Client with the given pool and client name
func NewClientFromPool(pool *redis.Pool, name string) *Client {
	ret := &Client{
		Pool: pool,
		Name: name,
	}
	return ret
}

// Reserve - Creates an empty Bloom Filter with a given desired error ratio and initial capacity.
// args:
// key - the name of the filter
// error_rate - the desired probability for false positives
// capacity - the number of entries you intend to add to the filter
func (client *Client) Reserve(key string, error_rate float64, capacity uint64) (err error) {
	conn := client.Pool.Get()
	defer conn.Close()
	_, err = conn.Do("BF.RESERVE", key, strconv.FormatFloat(error_rate, 'g', 16, 64), capacity)
	return err
}

// Add - Add (or create and add) a new value to the filter
// args:
// key - the name of the filter
// item - the item to add
func (client *Client) Add(key string, item string) (exists bool, err error) {
	conn := client.Pool.Get()
	defer conn.Close()
	return redis.Bool(conn.Do("BF.ADD", key, item))
}

// Exists - Determines whether an item may exist in the Bloom Filter or not.
// args:
// key - the name of the filter
// item - the item to check for
func (client *Client) Exists(key string, item string) (exists bool, err error) {
	conn := client.Pool.Get()
	defer conn.Close()
	return redis.Bool(conn.Do("BF.EXISTS", key, item))
}

// Info - Return information about key
// args:
// key - the name of the filter
func (client *Client) Info(key string) (info map[string]int64, err error) {
	conn := client.Pool.Get()
	defer conn.Close()
	result, err := conn.Do("BF.INFO", key)
	if err != nil {
		return nil, err
	}

	values, err := redis.Values(result, nil)
	if err != nil {
		return nil, err
	}
	if len(values)%2 != 0 {
		return nil, errors.New("Info expects even number of values result")
	}
	info = map[string]int64{}
	for i := 0; i < len(values); i += 2 {
		key, err = redis.String(values[i], nil)
		if err != nil {
			return nil, err
		}
		info[key], err = redis.Int64(values[i+1], nil)
		if err != nil {
			return nil, err
		}
	}
	return info, nil
}

// BfAddMulti - Adds one or more items to the Bloom Filter, creating the filter if it does not yet exist.
// args:
// key - the name of the filter
// item - One or more items to add
func (client *Client) BfAddMulti(key string, items []string) ([]int64, error) {
	conn := client.Pool.Get()
	defer conn.Close()
	args := redis.Args{key}.AddFlat(items)
	result, err := conn.Do("BF.MADD", args...)
	return redis.Int64s(result, err)
}

// BfExistsMulti - Determines if one or more items may exist in the filter or not.
// args:
// key - the name of the filter
// item - one or more items to check
func (client *Client) BfExistsMulti(key string, items []string) ([]int64, error) {
	conn := client.Pool.Get()
	defer conn.Close()
	args := redis.Args{key}.AddFlat(items)
	result, err := conn.Do("BF.MEXISTS", args...)
	return redis.Int64s(result, err)
}

// Initializes a TopK with specified parameters.
func (client *Client) TopkReserve(key string, topk int64, width int64, depth int64, decay float64) (string, error) {
	conn := client.Pool.Get()
	defer conn.Close()
	result, err := conn.Do("TOPK.RESERVE", key, topk, width, depth, strconv.FormatFloat(decay, 'g', 16, 64))
	return redis.String(result, err)
}

// Adds an item to the data structure.
func (client *Client) TopkAdd(key string, items []string) ([]string, error) {
	conn := client.Pool.Get()
	defer conn.Close()
	args := redis.Args{key}.AddFlat(items)
	result, err := conn.Do("TOPK.ADD", args...)
	return redis.Strings(result, err)
}

// Returns count for an item.
func (client *Client) TopkCount(key string, items []string) ([]string, error) {
	conn := client.Pool.Get()
	defer conn.Close()
	args := redis.Args{key}.AddFlat(items)
	result, err := conn.Do("TOPK.COUNT", args...)
	return redis.Strings(result, err)
}

// Checks whether an item is one of Top-K items.
func (client *Client) TopkQuery(key string, items []string) ([]int64, error) {
	conn := client.Pool.Get()
	defer conn.Close()
	args := redis.Args{key}.AddFlat(items)
	result, err := conn.Do("TOPK.QUERY", args...)
	return redis.Int64s(result, err)
}

// Return full list of items in Top K list.
func (client *Client) TopkList(key string) ([]string, error) {
	conn := client.Pool.Get()
	defer conn.Close()
	result, err := conn.Do("TOPK.LIST", key)
	return redis.Strings(result, err)
}

// Returns number of required items (k), width, depth and decay values.
func (client *Client) TopkInfo(key string) (map[string]string, error) {
	conn := client.Pool.Get()
	defer conn.Close()
	reply, err := conn.Do("TOPK.INFO", key)
	values, err := redis.Values(reply, err)
	if err != nil {
		return nil, err
	}
	if len(values)%2 != 0 {
		return nil, errors.New("expects even number of values result")
	}

	m := make(map[string]string, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		k := values[i].(string)
		switch v := values[i+1].(type) {
		case []byte:
			m[k] = string(values[i+1].([]byte))
			break
		case int64:
			m[k] = strconv.FormatInt(values[i+1].(int64), 10)
		default:
			return nil, fmt.Errorf("unexpected element type for (Ints,String), got type %T", v)
		}
	}
	return m, err
}

// Increase the score of an item in the data structure by increment.
func (client *Client) TopkIncrBy(key string, itemIncrements map[string]int64) ([]string, error) {
	conn := client.Pool.Get()
	defer conn.Close()
	args := redis.Args{key}
	for k, v := range itemIncrements {
		args = args.Add(k, v)
	}
	reply, err := conn.Do("TOPK.INCRBY", args...)
	return redis.Strings(reply, err)
}
