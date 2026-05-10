package pikpak

import (
	"fmt"
	"strconv"
	"time"

	"github.com/video-site/backend/internal/drives"
)

type filesResp struct {
	Files         []file `json:"files"`
	NextPageToken string `json:"next_page_token"`
}

type file struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	Name           string    `json:"name"`
	CreatedTime    time.Time `json:"created_time"`
	ModifiedTime   time.Time `json:"modified_time"`
	Hash           string    `json:"hash"`
	Size           string    `json:"size"`
	ThumbnailLink  string    `json:"thumbnail_link"`
	WebContentLink string    `json:"web_content_link"`
	Medias         []media   `json:"medias"`
}

type media struct {
	Link struct {
		URL    string    `json:"url"`
		Token  string    `json:"token"`
		Expire time.Time `json:"expire"`
	} `json:"link"`
	IsDefault bool `json:"is_default"`
	IsOrigin  bool `json:"is_origin"`
	Priority  int  `json:"priority"`
}

type authResp struct {
	RefreshToken string `json:"refresh_token"`
	AccessToken  string `json:"access_token"`
	Sub          string `json:"sub"`
}

type errResp struct {
	ErrorCode        int64  `json:"error_code"`
	ErrorMsg         string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (e *errResp) isError() bool {
	return e.ErrorCode != 0 || e.ErrorMsg != "" || e.ErrorDescription != ""
}

func (e *errResp) Error() string {
	return fmt.Sprintf("pikpak error_code=%d error=%s description=%s", e.ErrorCode, e.ErrorMsg, e.ErrorDescription)
}

type captchaTokenRequest struct {
	Action       string            `json:"action"`
	CaptchaToken string            `json:"captcha_token"`
	ClientID     string            `json:"client_id"`
	DeviceID     string            `json:"device_id"`
	Meta         map[string]string `json:"meta"`
	RedirectURI  string            `json:"redirect_uri"`
}

type captchaTokenResponse struct {
	CaptchaToken string `json:"captcha_token"`
	ExpiresIn    int64  `json:"expires_in"`
	URL          string `json:"url"`
}

func fileToEntry(f file, parentID string) drives.Entry {
	size, _ := strconv.ParseInt(f.Size, 10, 64)
	return drives.Entry{
		ID:           f.ID,
		Name:         f.Name,
		Size:         size,
		IsDir:        f.Kind == "drive#folder",
		ParentID:     parentID,
		MimeType:     guessMime(f.Name),
		ModTime:      f.ModifiedTime,
		ThumbnailURL: f.ThumbnailLink,
	}
}
