package guardian

import (
	"fmt"
	lru "github.com/hashicorp/golang-lru"
	"net"
	"sync"
	"time"
)

type Prisoner struct {
	IP     net.IP    `yaml:"ip" json:"ip"`
	Jail   Jail      `yaml:"jail" json:"jail"`
	Expiry time.Time `yaml:"expiry" json:"expiry"`
}

type prisonersCache struct {
	// cache is a lru cache safe for concurrent use by multiple go routines.
	cache *lru.Cache

	// we need a mutex in order to serialize the execution of multiple actions to the cache (e.g. get all prisoners)
	mutex sync.RWMutex
}

func (pc *prisonersCache) addPrisoner(remoteAddress string, jail Jail) Prisoner {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()
	ip := net.ParseIP(remoteAddress)
	p := Prisoner{
		IP:     ip,
		Jail:   jail,
		Expiry: time.Now().UTC().Add(jail.BanDuration),
	}

	pc.cache.Add(to4(ip), p)
	return p
}

func (pc *prisonersCache) addPrisonerFromStore(prisoner Prisoner) {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()
	pc.cache.Add(to4(prisoner.IP), prisoner)
}

func (pc *prisonersCache) isPrisoner(remoteAddress string) bool {
	pc.mutex.RLock()
	defer pc.mutex.RUnlock()
	_, found := pc.cache.Get(to4(net.ParseIP(remoteAddress)))
	return found
}

func (pc *prisonersCache) getPrisoner(remoteAddress string) (Prisoner, bool) {
	pc.mutex.RLock()
	defer pc.mutex.RUnlock()
	p, found := pc.cache.Get(to4(net.ParseIP(remoteAddress)))
	if !found {
		return Prisoner{}, false
	}
	prisoner, ok := p.(Prisoner)
	if !ok {
		return Prisoner{}, false
	}
	return prisoner, true
}

func (pc *prisonersCache) removePrisoner(remoteAddress string) bool {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()
	return pc.cache.Remove(to4(net.ParseIP(remoteAddress).To4()))
}

func (pc *prisonersCache) length() int {
	pc.mutex.RLock()
	defer pc.mutex.RUnlock()
	return pc.cache.Len()
}

func (pc *prisonersCache) getPrisoners() []Prisoner {
	pc.mutex.RLock()
	defer pc.mutex.RUnlock()
	prisoners := []Prisoner{}
	keys := pc.cache.Keys()
	for _, key := range keys {
		p, ok := pc.cache.Get(key)
		if !ok {
			continue
		}
		prisoner, ok := p.(Prisoner)
		if !ok {
			continue
		}
		prisoners = append(prisoners, prisoner)
	}
	return prisoners
}

func (pc *prisonersCache) purge() {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()
	pc.cache.Purge()
}

func to4(ip net.IP) [4]byte {
	var arr [4]byte
	copy(arr[:], ip.To4())
	return arr
}

func newPrisonerCache(maxSize int) (*prisonersCache, error) {
	cache, err := lru.New(maxSize)
	if err != nil {
		return nil, fmt.Errorf("unable to create PrisonerCache: %v", err)
	}
	return &prisonersCache{cache, sync.RWMutex{}}, nil
}
