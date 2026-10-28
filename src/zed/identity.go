package zed

import (
	"crypto/md5"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"

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

// identityFor returns a stable, human readable name and colour for a user id.
// The name is derived from the id so that the same client keeps the same
// identity across restarts, which lets people tell their sessions apart.
func identityFor(userId uint64) (name string, initials string, color string) {
	sum := md5.Sum([]byte(fmt.Sprintf("zedex-user-%d", userId)))
	adjective := identityAdjectives[int(sum[0])%len(identityAdjectives)]
	noun := identityNouns[int(sum[1])%len(identityNouns)]
	name = fmt.Sprintf("%s-%s", adjective, noun)

	h := fnv.New32a()
	h.Write([]byte(fmt.Sprintf("color-%d", userId)))
	c := h.Sum32()

	words := strings.Split(name, "-")
	initials = strings.ToUpper(string([]rune(words[0])[0]) + string([]rune(words[1])[0]))

	// Keep the colours dark enough to work on Zed's light theme too.
	hue := c % 360
	saturation := 45 + (c/360)%20
	lightness := 40 + (c/720)%15
	color = fmt.Sprintf("hsl(%d, %d%%, %d%%)", hue, saturation, lightness)
	return name, initials, color
}

// Avatar renders a small identicon for a user so that clients can show who
// they are signed in as.
func (co *Controller) Avatar(c *gin.Context) {
	id := c.Param("id")
	var userId uint64
	if _, err := fmt.Sscanf(id, "%d", &userId); err != nil {
		c.String(http.StatusBadRequest, "invalid user id")
		return
	}
	name, initials, color := identityFor(userId)

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="128" height="128" viewBox="0 0 128 128">
  <rect width="128" height="128" rx="64" fill="%s"/>
  <text x="64" y="64" font-family="sans-serif" font-size="52" font-weight="600" fill="#ffffff" text-anchor="middle" dominant-baseline="central">%s</text>
</svg>`, color, initials)

	c.Data(http.StatusOK, "image/svg+xml; charset=utf-8", []byte(svg))
	_ = name
}
