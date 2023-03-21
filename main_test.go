package main

import (
	"testing"

	"github.com/AustinMCrane/tcgplayer"
	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGetGroups(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	client := NewMockTcgplayer(ctrl)

	client.EXPECT().GetGroups(tcgplayer.GroupParams{
		CategoryID: tcgplayer.CategoryYugioh,
		Limit:      100,
		Offset:     0,
	}).Return([]*tcgplayer.Group{{Name: "test-group"}}, nil)

	groups, err := getGroups(client)
	require.NoError(t, err)
	require.Len(t, groups, 1)
}
