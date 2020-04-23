package guardian

import (
	"fmt"
	lru "github.com/hashicorp/golang-lru"
	"net"
	"time"
)

type Prisoner struct {
	IP     net.IP    `yaml:"ip" json:"ip"`
	Jail   Jail      `yaml:"jail" json:"jail"`
	Expiry time.Time `yaml:"expiry" json:"expiry"`
}

// prisonersCache maintains a cache of prisoners with a max size.
type prisonersCache struct {
	// cache is a lru cache safe for concurrent use by multiple go routines.
	cache *lru.Cache
}

func (pc *prisonersCache) addPrisoner(remoteAddress string, jail Jail) Prisoner {
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
	pc.cache.Add(to4(prisoner.IP), prisoner)
}

func (pc *prisonersCache) isPrisoner(remoteAddress string) bool {
	_, found := pc.cache.Get(to4(net.ParseIP(remoteAddress)))
	return found
}

func (pc *prisonersCache) getPrisoner(remoteAddress string) (Prisoner, bool) {
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
	return pc.cache.Remove(to4(net.ParseIP(remoteAddress).To4()))
}

func (pc *prisonersCache) length() int {
	return pc.cache.Len()
}

func (pc *prisonersCache) purge() {
	pc.cache.Purge()
}

func to4(ip net.IP) [4]byte {
	var arr [4]byte
	copy(arr[:], ip.To4())
	return arr
}

func newPrisonerCache(maxSize uint16) (*prisonersCache, error) {
	cache, err := lru.New(int(maxSize))
	if err != nil {
		return nil, fmt.Errorf("unable to create PrisonerCache: %v", err)
	}
	return &prisonersCache{cache}, nil
}
