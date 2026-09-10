package imagemounter

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  response
	}{
		{
			name:  "success without request message",
			input: "STATUS=0&MESSAGE=SUCCESS",
			want: response{
				status:  0,
				message: "SUCCESS",
			},
		},
		{
			name:  "response with multiword status",
			input: "STATUS=69&MESSAGE=This device isn't eligible for the requested build.",
			want: response{
				status:  69,
				message: "This device isn't eligible for the requested build.",
			},
		},
		{
			name:  "response with request string",
			input: "STATUS=0&MESSAGE=SUCCESS&REQUEST_STRING=<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>",
			want: response{
				status:        0,
				message:       "SUCCESS",
				requestString: "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			r, err := parseResponse(reader)
			require.NoError(t, err)
			assert.Equal(t, tt.want, r)
		})
	}
}

func TestBuildSignatureRequestAlwaysMarksEntriesProductionAndSecure(t *testing.T) {
	// Arrange: DDI 的 BuildManifest 条目不带 EPRO / ESEC，解析后是零值
	identity := buildIdentity{
		Manifest: map[string]manifestEntry{
			"LoadableTrustCache": {Digest: []byte{0x01}, Trusted: true},
			"PersonalizedDMG":    {Digest: []byte{0x02}, Trusted: true, Name: "DeveloperDiskImage"},
			"UntrustedThing":     {Digest: []byte{0x03}, Trusted: false},
		},
	}
	identifiers := personalizationIdentifiers{
		BoardId:               0x0c,
		ChipID:                0x8150,
		SecurityDomain:        1,
		AdditionalIdentifiers: map[string]interface{}{"Ap,SikaFuse": uint64(0)},
	}

	// Act
	params := buildSignatureRequest(identity, identifiers, make([]byte, 48), 1234)

	// Assert
	for _, key := range []string{"LoadableTrustCache", "PersonalizedDMG"} {
		entry, ok := params[key].(map[string]interface{})
		require.True(t, ok, "missing entry %s", key)
		assert.Equal(t, true, entry["EPRO"], "%s must be flagged production, otherwise TSS answers STATUS=94", key)
		assert.Equal(t, true, entry["ESEC"], "%s must be flagged secure, otherwise TSS answers STATUS=94", key)
		assert.Equal(t, true, entry["Trusted"])
	}
	assert.Equal(t, "DeveloperDiskImage", params["PersonalizedDMG"].(map[string]interface{})["Name"])
	assert.NotContains(t, params, "UntrustedThing")
	assert.Equal(t, uint64(0), params["Ap,SikaFuse"])
	assert.Equal(t, true, params["ApProductionMode"])
	assert.Equal(t, true, params["ApSecurityMode"])
}

func TestDeviceRejectionMapsAlreadyMountedToSentinel(t *testing.T) {
	alreadyMounted := `Error Domain=com.apple.MobileStorage.ErrorDomain Code=-3 "A disk image of type Personalized/DeveloperDiskImage is already mounted at /System/Developer."`

	err := deviceRejection("mountPersonalizedImage", "ImageMountFailed", alreadyMounted)
	assert.True(t, errors.Is(err, ErrAlreadyMounted), "already-mounted rejection must map to ErrAlreadyMounted")
	assert.Contains(t, err.Error(), "ImageMountFailed")

	other := deviceRejection("mountImage", "ImageMountFailed", "Error Domain=com.apple.MobileStorage.ErrorDomain Code=-2 \"Failed to mount\"")
	assert.False(t, errors.Is(other, ErrAlreadyMounted))

	// DetailedError 不是字符串时不能 panic，也不能误判成 already mounted
	nonString := deviceRejection("mountImage", "ImageMountFailed", nil)
	assert.False(t, errors.Is(nonString, ErrAlreadyMounted))
}

// TODO: It looks like `REQUEST_STRING` always comes last, but if that's not the case we are not sure what to do
// as it could also contain the '&' separator character
func TestParseResponseRequiresRequestStringLast(t *testing.T) {
	_, err := parseResponse(strings.NewReader("REQUEST_STRING=abc&STATUS=0"))
	assert.Error(t, err)
}
