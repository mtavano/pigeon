package scheduler

import (
	"log"
	"net/url"

	"github.com/garyburd/redigo/redis"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
)

// TODO: Add retry logic and only panic if connection is unrecoverable.

var (
	scripts map[string]*redis.Script

	scriptsSources = map[string]string{
		"pop": `
			local result_set = redis.call('ZRANGE', 'pq:ids', 0, 0)
			if not result_set or #result_set == 0 then
				return ''
			end

			redis.call('ZREMRANGEBYRANK', 'pq:ids', 0, 0)

			return result_set[1]
		`,
		"push": `
			local timestamp = ARGV[1]
			local id = ARGV[2]

			redis.call('ZADD', 'pq:ids', timestamp, id)

			return true
		`,
		"peek": `
			local result_set = redis.call('ZRANGE', 'pq:ids', 0, 0)
			if not result_set or #result_set == 0 then
				return false
			end
			return result_set[1]
		`,
		"delete": `
			local id = ARGV[1]

			local result_set = redis.call('DEL', 'pq:ids', id)

			return result_set
		`,
	}
)

func init() {
	scripts = make(map[string]*redis.Script)

	for k, v := range scriptsSources {
		scripts[k] = redis.NewScript(0, v)
	}
}

type priorityQueue struct {
	pool interface {
		Get() redis.Conn
	}
}

func newPriorityQueue(config StorageConfig) *priorityQueue {
	log.Println(config)
	pool := &redis.Pool{
		Dial:        dial(config),
		MaxIdle:     config.RedisMaxIdle,
		IdleTimeout: config.RedisIdleTimeout,
	}

	conn := pool.Get()
	if err := conn.Err(); err != nil {
		panic(err)
	}
	conn.Close()

	return &priorityQueue{pool}
}

func (pq *priorityQueue) Push(id ulid.ULID) {
	conn := pq.pool.Get()
	defer conn.Close()

	_, err := scripts["push"].Do(conn, id.Time(), id.String())
	if err != nil {
		panic(err)
	}
}

func (pq *priorityQueue) Peek() *ulid.ULID {
	conn := pq.pool.Get()
	defer conn.Close()

	idStr, err := redis.String(scripts["peek"].Do(conn))
	if err != nil {
		if err == redis.ErrNil {
			return nil
		}
		panic(err)
	}

	id, err := ulid.Parse(idStr)
	if err != nil {
		panic(err)
	}
	return &id
}

func (pq *priorityQueue) Pop() (*ulid.ULID, error) {
	conn := pq.pool.Get()
	defer conn.Close()

	idStr, err := redis.String(scripts["pop"].Do(conn))
	if err != nil {
		return nil, err
	}

	if idStr == "" {
		return nil, errors.New("not found message at pop on priority queue")
	}

	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, err
		panic(err)
	}

	return &id, nil
}

// DeleteByID
func (pq *priorityQueue) DeleteByID(id ulid.ULID) (bool, error) {
	conn := pq.pool.Get()
	defer conn.Close()

	// TODO: check for casting
	res, err := redis.Int(scripts["delete"].Do(conn, id.String()))
	if err != nil {
		return false, err
	}

	if res == 0 {
		return false, nil
	}

	return true, nil
}

func dial(config StorageConfig) func() (redis.Conn, error) {
	return func() (redis.Conn, error) {
		u, err := url.Parse(config.RedisURL)
		if err != nil {
			return nil, err
		}

		conn, err := redis.Dial("tcp", u.Host)
		if err != nil {
			log.Printf("reymonkey")
			return nil, err
		}
		if _, err = conn.Do("SELECT", config.RedisDatabase); err != nil {
			conn.Close()
			return nil, err
		}

		if u.User == nil {
			return conn, nil
		}
		if pw, ok := u.User.Password(); ok {
			if _, err = conn.Do("AUTH", pw); err != nil {
				conn.Close()
				return nil, err
			}
		}
		return conn, nil
	}
}
