// Media in the Graph API: upload, description, and download.
//
// THE TRAP THIS FILE EXISTS TO NOT REPEAT: Meta reports the SAME media with
// DIFFERENT mimes in two places — `audio/ogg; codecs=opus` in the message
// payload (parse.go, Event.MediaMimePayload) and `audio/ogg` in `GET
// /{media_id}` (DescribeMedia, here). It's the `; codecs=opus` that makes
// WhatsApp render a PLAYABLE VOICE NOTE; resending with the other one
// delivers a FILE ATTACHMENT — and the message arrives, with no error at
// all, nowhere. Cost paid in production on this network on 2026-07-20
// (docs/ARMADILHAS.md).
//
// Consequence in code, and it holds in BOTH directions: no function in this
// file NORMALIZES a mime. On upload, the mime the consumer declared goes
// over the wire AS IT CAME, parameter included; on download, the mime Meta
// reports goes up RAW. The category table below reads the BASE mime to
// decide the ceiling and body shape, but never rewrites the value that
// travels.
package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

var (
	// ErrUnsupportedMime: the mime isn't in this file's table.
	ErrUnsupportedMime = errors.New("meta: mime nao suportado")
	// ErrUploadWithoutID: Meta answered 2xx and didn't send a media_id. Same
	// trap (and same outcome) as ErrResponseWithoutID on send.
	ErrUploadWithoutID = errors.New("meta: resposta 2xx sem media_id")
	// ErrInvalidMediaID: the id has a shape that cannot become a URL
	// segment.
	ErrInvalidMediaID = errors.New("meta: media_id com forma invalida")
	// ErrBadMediaURL: the download URL Meta returned is no good.
	ErrBadMediaURL = errors.New("meta: url de download da midia invalida")
)

// Category is OUR OWN name for the media family. It appears in the
// consumer contract (`"categoria": "audio"`), so it's Portuguese;
// GraphAPIType does the translation to Meta's name in a single place.
type Category string

const (
	CategoryImage    Category = "imagem"
	CategoryVideo    Category = "video"
	CategoryAudio    Category = "audio"
	CategoryDocument Category = "documento"
	CategorySticker  Category = "sticker"
)

// categoryRules is everything the rest of the project needs to know
// about a category. A single table, on purpose: whoever validates
// (outbound/mensagem.go), whoever builds the body (outbound/corpo.go), and
// whoever rejects before the wire (outbound/media_handler.go) read FROM
// HERE. Two tables for the same question diverge at the first change — this
// project's mother trap.
type categoryRules struct {
	// graphType is the field/type name in the Graph API ("image",
	// "document"...).
	graphType string
	// maxBytes is OUR OWN, not Meta's. See the comment on tetos, below.
	maxBytes int64
	// acceptsCaption/acceptsFilename say which fields the built body has
	// somewhere to put. Accepting a field the body doesn't carry would
	// SILENTLY DISCARD it — this project's most expensive failure shape.
	acceptsCaption  bool
	acceptsFilename bool
	// mimes is the list of BASE mimes (no parameter) for this category.
	mimes []string
}

// THE CEILINGS BELOW ARE OURS, NOT META'S.
//
// I did not find — and that's why I don't assert — an official per-category
// number in a source verified at the time this line was written. Inventing
// a number that looks official would be worse than having no number at
// all: someone would trust it. So these are CONSERVATIVE ceilings chosen
// here, and the rejection says so to the consumer ("the ceiling is the
// gateway's").
//
// They exist for a concrete reason: without a ceiling, sending 40 MB to
// find out Meta rejects it costs bandwidth and time on both sides, and its
// rejection arrives after the whole upload. If Meta publishes (or usage
// shows) a higher limit, raising a number here is one line.
//
// THE MIME LIST IS ALSO OURS and is conservative: it rejects what we don't
// know instead of sending and hoping. Adding a mime is one line; what it
// cannot do is have the list lie by claiming to be "what Meta accepts".
const (
	oneKiB = 1 << 10
	oneMiB = 1 << 20
)

var categories = map[Category]categoryRules{
	CategoryImage: {
		graphType: "image", maxBytes: 5 * oneMiB, acceptsCaption: true,
		mimes: []string{"image/jpeg", "image/png"},
	},
	CategoryVideo: {
		graphType: "video", maxBytes: 16 * oneMiB, acceptsCaption: true,
		mimes: []string{"video/mp4", "video/3gpp"},
	},
	CategoryAudio: {
		// NO caption: the Graph API's audio body has no caption, and an
		// accepted field the body doesn't carry would vanish with no
		// error.
		graphType: "audio", maxBytes: 16 * oneMiB,
		mimes: []string{"audio/aac", "audio/amr", "audio/mpeg", "audio/mp4", "audio/ogg"},
	},
	CategoryDocument: {
		graphType: "document", maxBytes: 32 * oneMiB, acceptsCaption: true, acceptsFilename: true,
		mimes: []string{
			"application/pdf",
			"application/msword",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"application/vnd.ms-excel",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			"application/vnd.ms-powerpoint",
			"application/vnd.openxmlformats-officedocument.presentationml.presentation",
			"text/plain",
			"text/csv",
		},
	},
	CategorySticker: {
		// `image/webp` lives HERE and not under image: on WhatsApp webp is
		// a sticker. A mime in two categories would make CategoryOfMime
		// ambiguous — see the index below, which rejects the ambiguity
		// instead of choosing on its own.
		graphType: "sticker", maxBytes: 500 * oneKiB,
		mimes: []string{"image/webp"},
	},
}

// categoryByMime is the reverse index, built ONCE from the table above —
// never written by hand, or the two lists would diverge.
//
// The panic is deliberate and is not a production risk: it only fires if
// this file's table declares the same mime in two categories, which is a
// code defect, deterministic, and shows up on the FIRST run of any test.
// The alternative — "last one wins", silently — is exactly the failure
// shape this project chases down.
var categoryByMime = indexMimes()

func indexMimes() map[string]Category {
	index := make(map[string]Category)
	for cat, rules := range categories {
		for _, m := range rules.mimes {
			if other, repeated := index[m]; repeated {
				panic(fmt.Sprintf("meta: mime %q declarado em %q e %q", m, other, cat))
			}
			index[m] = cat
		}
	}
	return index
}

// CategoryOfMime decides the category by the BASE mime, without touching
// the value that travels. Searching a table for a raw "audio/ogg;
// codecs=opus" would find nothing and would reject precisely the voice
// note — and stripping the parameter to "fix" that would destroy what the
// upload needs to send.
func CategoryOfMime(mimeType string) (Category, error) {
	base, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return "", fmt.Errorf("%w: %q nao e um mime valido", ErrUnsupportedMime, mimeType)
	}
	cat, known := categoryByMime[base]
	if !known {
		return "", fmt.Errorf("%w: %q", ErrUnsupportedMime, base)
	}
	return cat, nil
}

// CategoryCap returns OUR ceiling in bytes. Zero for a category that
// doesn't exist — the caller has already gone through CategoryOfMime or
// KnownCategory.
func CategoryCap(c Category) int64 { return categories[c].maxBytes }

// KnownCategory translates the text the consumer sent.
func KnownCategory(name string) (Category, bool) {
	c := Category(strings.TrimSpace(name))
	_, exists := categories[c]
	return c, exists
}

// GraphAPIType is the ONLY translation between our name and Meta's.
func GraphAPIType(c Category) string { return categories[c].graphType }

// AcceptsCaption/AcceptsFilename say whether the built body has somewhere
// to put the field. They're consulted by whoever VALIDATES and by whoever
// BUILDS: if each one had its own rule, one would accept what the other
// throws away, and the consumer would see the text vanish with no error at
// all.
func AcceptsCaption(c Category) bool { return categories[c].acceptsCaption }

func AcceptsFilename(c Category) bool { return categories[c].acceptsFilename }

// MediaIDValid uses THE SAME rule as PhoneNumberIDValid (client.go), and
// deliberately doesn't copy it: the rule is about safely becoming a URL
// segment (url.JoinPath resolves `..` like path.Join), and a copy would
// diverge from it at the first change.
func MediaIDValid(id string) bool { return PhoneNumberIDValid(id) }

// UploadMedia sends the bytes to `POST /{phone_number_id}/media` and
// returns the media_id.
//
// THE BYTES CROSS IN STREAMING. The multipart is written into an io.Pipe
// and http.Client reads the other end while the consumer is still sending —
// no ReadAll, no temp file, nothing in the log. Media is CONTENT, and this
// project's line is "configuration yes, message never": a 16 MB temp file
// with a client's audio is exactly what cannot exist.
//
// `mimeType` goes over the wire AS IT CAME, parameter included. See the top
// of the file.
func (c *Client) UploadMedia(
	ctx context.Context, phoneNumberID, token, mimeType, filename string, content io.Reader,
) (string, error) {
	if !PhoneNumberIDValid(phoneNumberID) {
		return "", ErrInvalidPhoneNumberID
	}
	target, err := url.JoinPath(c.base, phoneNumberID, "media")
	if err != nil {
		return "", fmt.Errorf("meta: montar url: %w", err)
	}

	reader, writer := io.Pipe()
	multi := multipart.NewWriter(writer)
	go func() {
		// CloseWithError(nil) and Close: the error (if any) travels
		// through the pipe and aborts the HTTP request, instead of
		// sending a truncated body Meta would accept as an incomplete
		// file.
		var errWrite error
		defer func() { _ = writer.CloseWithError(errWrite) }()

		if errWrite = multi.WriteField("messaging_product", "whatsapp"); errWrite != nil {
			return
		}
		if errWrite = multi.WriteField("type", mimeType); errWrite != nil {
			return
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition",
			fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
		// The part's Content-Type also carries the FULL mime:
		// CreateFormFile would write "application/octet-stream" here, and
		// the parameter would die precisely on the field that describes
		// the file.
		header.Set("Content-Type", mimeType)

		var part io.Writer
		if part, errWrite = multi.CreatePart(header); errWrite != nil {
			return
		}
		if _, errWrite = io.Copy(part, content); errWrite != nil {
			return
		}
		errWrite = multi.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, reader)
	if err != nil {
		_ = reader.CloseWithError(err) // frees the goroutine that's writing
		return "", fmt.Errorf("meta: montar requisicao: %w", err)
	}
	req.Header.Set("Content-Type", multi.FormDataContentType())
	// The token goes in the HEADER, never in the URL: a token in a query
	// string leaks into proxy, server, and CDN logs.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		// We do NOT interpolate the error: *url.Error carries the full
		// URL, and it carries the client's phone_number_id.
		return "", fmt.Errorf("meta: falha de transporte no upload: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return "", fmt.Errorf("meta: ler resposta do upload: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return "", metaError
	}
	return uploadID(raw)
}

// uploadID requires an `id` that's a non-empty string. THE SAME trap as
// sending: a 2xx from Meta doesn't prove an id came with it, and returning
// "" as success would make the consumer store a useless media_id — the
// defect would show up far from here, when it's time to send the message.
func uploadID(raw []byte) (string, error) {
	var envelope struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", fmt.Errorf("%w: corpo nao entendido", ErrUploadWithoutID)
	}
	id := strings.TrimSpace(envelope.ID)
	if id == "" {
		return "", ErrUploadWithoutID
	}
	return id, nil
}

// Media is what `GET /{media_id}` reports about a media file.
//
// MimeFromGet is HALF of a pair: the other half is Event.MediaMimePayload,
// which came in the message webhook. Both describe the SAME media and are
// DIFFERENT. This type doesn't keep the other half on purpose — the gateway
// doesn't keep the message, so whoever has the payload's mime is the
// consumer, and it's them who chooses which to use when resending.
type Media struct {
	// MimeFromGet is what Meta reported, RAW.
	MimeFromGet string
	// SizeBytes and SHA256 come from it and travel as information; the
	// gateway doesn't decide anything based on them.
	SizeBytes int64
	SHA256    string
	// URL is temporary and requires the instance's token. It is NOT passed
	// through to the consumer: handing over the URL would hand over the
	// need for the token along with it.
	URL string
}

// DescribeMedia reads the media's description. It does NOT download the
// bytes.
func (c *Client) DescribeMedia(ctx context.Context, mediaID, token string) (Media, error) {
	if !MediaIDValid(mediaID) {
		return Media{}, ErrInvalidMediaID
	}
	target, err := url.JoinPath(c.base, mediaID)
	if err != nil {
		return Media{}, fmt.Errorf("meta: montar url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Media{}, fmt.Errorf("meta: montar requisicao: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return Media{}, fmt.Errorf("meta: falha de transporte ao descrever a midia: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return Media{}, fmt.Errorf("meta: ler descricao da midia: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return Media{}, metaError
	}

	// Field by field, never a single Unmarshal straight into a struct: a
	// field with an unexpected type would zero the WHOLE struct and take
	// the url and the mime down with it. It's the same trap that already
	// cost the webhook parser a Critical.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Media{}, fmt.Errorf("%w: a descricao nao e um objeto JSON", ErrBadMediaURL)
	}
	m := Media{
		URL:         textOf(fields["url"]),
		MimeFromGet: textOf(fields["mime_type"]), // RAW, with parameter if it sends one
		SHA256:      textOf(fields["sha256"]),
		SizeBytes:   int64(tolerantInt(fields["file_size"])),
	}
	return m, nil
}

func textOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// OpenMedia opens the bytes. The caller CLOSES the returned body.
//
// The URL comes FROM OUTSIDE — from Meta's response body — and carries the
// instance's token in the header. That's why it's checked before any
// connection: an `http://` there would hand the token over in the clear to
// anyone on the path, and this project's rule is that TLS has no off mode,
// in both directions (CLAUDE.md).
func (c *Client) OpenMedia(ctx context.Context, m Media, token string) (io.ReadCloser, error) {
	target, err := url.Parse(strings.TrimSpace(m.URL))
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return nil, fmt.Errorf("%w: so https com host", ErrBadMediaURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("meta: montar requisicao: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meta: falha de transporte ao baixar a midia: %w", errWithoutDetail(err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The body is only read here: it's an ERROR body, small, and
		// without it there's no way to classify. On the success path the
		// body goes up intact to the caller, without passing through any
		// buffer.
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
		resp.Body.Close()
		return nil, ClassifyResponse(resp.StatusCode, raw)
	}
	return resp.Body, nil
}
