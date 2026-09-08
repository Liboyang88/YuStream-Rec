package domain

import (
	"strconv"
)

func KeyUserNameToID(username string) string {
	return "user:" + username
}

func KeyUserIDToInfo(id uint) string {
	return "user:" + strconv.Itoa(int(id))
}

func KeyItemIDToItem(id uint) string {
	return "item:" + strconv.Itoa(int(id))
}

func KeyPageIDToItems(id uint) string {
	return "page:" + strconv.Itoa(int(id)) + ":items"
}

func KeyUserIDToInteractions(id uint) string {
	return "user:" + strconv.Itoa(int(id)) + ":interactions"
}

func KeyUserIDToIaPtr(id uint) string {
	return "user:" + strconv.Itoa(int(id)) + ":interactions:ptr"
}

func RecentlyRecommended(id uint) string {
	return "user:" + strconv.Itoa(int(id)) + ":recentlyrecommended"
}
