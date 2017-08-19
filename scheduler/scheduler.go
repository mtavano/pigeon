package scheduler

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/WiseGrowth/pigeon/pigeon"
	pb "github.com/WiseGrowth/pigeon/pigeon/proto"
	"github.com/boltdb/bolt"
	"github.com/gogo/protobuf/proto"
	"github.com/oklog/ulid"
)

// TODO(ja): remove this struct.

// StorageConfig is a struct that will be deleted.
type StorageConfig struct {
	BoltDatabase     string        // File to use as bolt database.
	RedisURL         string        // URL of the redis server
	RedisLog         bool          // log database commands
	RedisMaxIdle     int           // maximum number of idle connections in the pool
	RedisDatabase    int           // redis database to use
	RedisIdleTimeout time.Duration // timeout for idle connections
}

// New builds a new pigeon.Store backed by bolt DB.
//
// In case of any error it panics.
func New(config StorageConfig) pigeon.SchedulerService {
	db, err := bolt.Open(config.BoltDatabase, os.ModePerm, nil)
	if err != nil {
		panic(err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, berr := tx.CreateBucketIfNotExists(msgBucket)
		return berr
	})
	if err != nil {
		panic(err)
	}

	s := &service{
		db:  db,
		pq:  newPriorityQueue(config),
		idc: make(chan ulid.ULID),
	}

	go s.run()

	return s
}

var msgBucket = []byte("messages")

type service struct {
	db *bolt.DB
	pq *priorityQueue

	idc chan ulid.ULID
}

func (s *service) Put(id ulid.ULID, content []byte, endpoint pigeon.NetAddr) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(msgBucket)

		k, merr := id.MarshalBinary()
		if merr != nil {
			return merr
		}
		v, jerr := proto.Marshal(&pb.Message{
			Id:       id.String(),
			Content:  content,
			Endpoint: string(endpoint),
		})
		if jerr != nil {
			return jerr
		}
		return b.Put(k, v)
	})
	if err != nil {
		return err
	}

	s.idc <- id

	return nil
}

func (s *service) Get(id ulid.ULID) (*pigeon.Message, error) {
	var msg pb.Message
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(msgBucket)
		k, err := id.MarshalBinary()
		if err != nil {
			return err
		}
		v := b.Get(k)
		if err := proto.Unmarshal(v, &msg); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &pigeon.Message{
		ID:       id,
		Content:  msg.Content,
		Endpoint: pigeon.NetAddr(msg.Endpoint),
	}, nil
}

func (s *service) Update(id ulid.ULID, content []byte) error {
	var msg pb.Message
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(msgBucket)
		k, err := id.MarshalBinary()
		if err != nil {
			return err
		}
		v := b.Get(k)
		if err = proto.Unmarshal(v, &msg); err != nil {
			return err
		}
		msg.Content = content
		v, err = proto.Marshal(&msg)
		if err != nil {
			return err
		}
		return b.Put(k, v)
	})
}

// Run in its goroutine
func (s *service) run() {
	var next uint64
	var timer *time.Timer

	pq := s.pq
	for {
		var tick <-chan time.Time

		top := pq.Peek()
		if top != nil {
			if t := top.Time(); t < next || next == 0 {
				var delay int64
				now := ulid.Timestamp(time.Now())
				if t >= now {
					delay = int64(t - now)
				}

				if timer == nil {
					timer = time.NewTimer(time.Duration(delay) * time.Millisecond)
				} else {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer = time.NewTimer(time.Duration(delay) * time.Millisecond)
				}
			}
		}

		if timer != nil && top != nil {
			tick = timer.C
		}

		select {
		case <-tick:
			if id := pq.Pop(); id != nil {
				go s.send(*id)
			}
			next = 0
		case id := <-s.idc:
			pq.Push(id)
		}
	}
}

func (s *service) send(id ulid.ULID) {
	msg, err := s.Get(id)
	if err != nil {
		fmt.Println(err)
		return
	}

	http.Post(string(msg.Endpoint), "text/plain", bytes.NewReader(msg.Content))
}