package imagemounter

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindIdentityErrorIsDeviceNotInManifest(t *testing.T) {
	// Arrange
	manifest := buildManifest{BuildIdentities: []buildIdentity{{BoardID: "0x0A", ChipID: "0x8150"}}}

	// Act
	_, err := manifest.findIdentity(personalizationIdentifiers{BoardId: 0x0a, ChipID: 0x8160})

	// Assert
	assert.True(t, errors.Is(err, ErrDeviceNotInManifest))
}

func TestFindIdentityMatchesBoardAndChip(t *testing.T) {
	// Arrange
	manifest := buildManifest{BuildIdentities: []buildIdentity{{BoardID: "0x0A", ChipID: "0x8150"}}}

	// Act
	identity, err := manifest.findIdentity(personalizationIdentifiers{BoardId: 0x0a, ChipID: 0x8150})

	// Assert
	assert.Nil(t, err)
	assert.Equal(t, 0x8150, identity.ApChipID())
}
