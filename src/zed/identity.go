package zed

import (
	"crypto/rand"
	"fmt"
	"hash/fnv"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

var identityAdjectives = []string{
	"amber", "brisk", "calm", "dapper", "eager", "fuzzy", "gentle", "happy",
	"idle", "jolly", "keen", "lucid", "mellow", "nimble", "olive", "proud",
	"quiet", "rapid", "steady", "tidy", "upbeat", "vivid", "witty", "zesty",
}

var identityNouns = []string{
	"acorn", "beacon", "cedar", "delta", "ember", "fjord", "grove", "harbor",
	"island", "jetty", "kite", "ledger", "meadow", "nimbus", "onyx", "pylon",
	"quartz", "ridge", "summit", "tundra", "umbra", "vector", "willow", "zephyr",
}

// identity is the display identity of a user.
type identity struct {
	name     string
	initials string
	color    string
}

// identityRegistry assigns a random display identity to every user the first
// time it is seen. The identity then stays stable for the lifetime of the
// server, so that all clients agree on the name of a user, while a restart
// gives everyone a fresh name.
type identityRegistry struct {
	mu     sync.Mutex
	byUser map[uint64]identity
	used   map[string]bool
}

var identities = &identityRegistry{
	byUser: map[uint64]identity{},
	used:   map[string]bool{},
}

// identityFor returns the display identity of a user, assigning a random one
// the first time the user is seen.
func identityFor(userId uint64) (name string, initials string, color string) {
	id := identities.get(userId)
	return id.name, id.initials, id.color
}

func (r *identityRegistry) get(userId uint64) identity {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.byUser[userId]; ok {
		return id
	}
	id := r.assign()
	r.byUser[userId] = id
	return id
}

// assign picks a random, unused name.
func (r *identityRegistry) assign() identity {
	for attempt := 0; attempt < 100; attempt++ {
		name := fmt.Sprintf("%s-%s",
			identityAdjectives[randomIndex(len(identityAdjectives))],
			identityNouns[randomIndex(len(identityNouns))],
		)
		if r.used[name] {
			continue
		}
		r.used[name] = true
		return identity{
			name:     name,
			initials: initialsFor(name),
			color:    colorFor(name),
		}
	}
	// Fall back to a numeric suffix if we somehow exhausted the name space.
	name := fmt.Sprintf("user-%d", randomIndex(100000))
	return identity{name: name, initials: "US", color: colorFor(name)}
}

// initialsFor derives the initials shown on the avatar.
func initialsFor(name string) string {
	words := strings.Split(name, "-")
	initials := ""
	for _, word := range words {
		if word == "" {
			continue
		}
		initials += strings.ToUpper(string([]rune(word)[0]))
	}
	if initials == "" {
		return "??"
	}
	return initials
}

// colorFor derives a stable colour from a name. The colour is dark enough to
// work on Zed's light theme as well.
func colorFor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	c := h.Sum32()
	hue := c % 360
	saturation := 45 + (c/360)%20
	lightness := 40 + (c/720)%15
	return fmt.Sprintf("hsl(%d, %d%%, %d%%)", hue, saturation, lightness)
}

// randomIndex returns a random index in [0, n).
func randomIndex(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

// Avatar renders a small identicon for a user so that clients can show who
// they are signed in as.
func (co *Controller) Avatar(c *gin.Context) {
	id := c.Param("id")
	userId, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid user id")
		return
	}

	// A user that signed in with a provider keeps the initials of the name
	// of that account.
	name := ""
	if co.auth != nil {
		if profile, ok := co.auth.Profile(userId); ok {
			name = profile.Username
		}
	}
	if name == "" {
		name, _, _ = identityFor(userId)
	}

	initials := initialsFor(name)
	color := colorFor(name)

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="128" height="128" viewBox="0 0 128 128">
  <rect width="128" height="128" rx="64" fill="%s"/>
  <text x="64" y="64" font-family="sans-serif" font-size="52" font-weight="600" fill="#ffffff" text-anchor="middle" dominant-baseline="central">%s</text>
</svg>`, color, initials)

	c.Data(http.StatusOK, "image/svg+xml; charset=utf-8", []byte(svg))
}
