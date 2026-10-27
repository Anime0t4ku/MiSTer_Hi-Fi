package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Emby as a media server: jellyfin.go does the work for both (see its header);
// this file is what only Emby has, the sign-in. Emby has no Quick Connect, so
// the user name and password go to the server once, in exchange for a token,
// and are not stored.

// embySignIn exchanges a user name and password for a token and saves the config.
func (c JellyfinConfig) embySignIn(ctx context.Context, user, password string) (JellyfinConfig, error) {
	if strings.TrimSpace(user) == "" {
		return c, errors.New("enter the Emby user name")
	}
	c.Token, c.UserID = "", ""
	req, err := c.request(ctx, "POST", "/Users/AuthenticateByName", nil, map[string]string{"Username": user, "Pw": password})
	if err != nil {
		return c, err
	}
	var res struct {
		AccessToken string `json:"AccessToken"`
		User        struct {
			ID string `json:"Id"`
		} `json:"User"`
	}
	if err := c.do(req, 15*time.Second, &res); err != nil {
		if errors.Is(err, errNotSignedIn) {
			return c, errors.New("Emby refused the user name or password")
		}
		return c, err
	}
	if res.AccessToken == "" || !jellyfinID(res.User.ID) {
		return c, errors.New("emby: sign-in returned no token")
	}
	c.Token, c.UserID = res.AccessToken, res.User.ID
	return c, saveJellyfin(c)
}

// readPassword reads a line from the terminal without echoing it (stdin when it
// is not one, e.g. a pipe).
func readPassword() string {
	fd := int(os.Stdin.Fd())
	var old syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&old)))
	if e == 0 {
		noecho := old
		noecho.Lflag &^= syscall.ECHO
		syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&noecho)))
		defer func() {
			syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&old)))
			fmt.Println()
		}()
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

// runEmbyLoginCLI is `mister_hifi --emby-login http://host:8096 <user>`; the
// password is asked for (or read from stdin).
func runEmbyLoginCLI(args []string) {
	c, err := loadEmby()
	user := ""
	if len(args) > 0 {
		c.Server = strings.TrimRight(strings.TrimSpace(args[0]), "/")
		if len(args) > 1 {
			user = args[1]
		}
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "usage: mister_hifi --emby-login http://host:8096 <user>")
		os.Exit(2)
	}
	if !isHTTPURL(c.Server) || user == "" {
		fmt.Fprintln(os.Stderr, "usage: mister_hifi --emby-login http://host:8096 <user>")
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "Password for %s: ", user)
	pw := readPassword()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := c.embySignIn(ctx, user, pw); err != nil {
		fmt.Fprintln(os.Stderr, "emby:", err)
		os.Exit(1)
	}
	fmt.Println("Signed in. Saved to", embyConfigPath)
}

// handleEmbySignIn: POST {"user", "password"} from the web remote's settings.
func (w *webRemote) handleEmbySignIn(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", 405)
		return
	}
	if !settingsRequest(rw, r) {
		return
	}
	var in struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := decodeSettings(r, &in); err != nil {
		http.Error(rw, "invalid request", 400)
		return
	}
	cfg, err := loadEmby()
	if err != nil {
		http.Error(rw, "save the server address first: "+err.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if _, err := cfg.embySignIn(ctx, in.User, in.Password); err != nil {
		http.Error(rw, err.Error(), 502)
		return
	}
	writeJSON(rw, map[string]bool{"signed_in": true})
}
