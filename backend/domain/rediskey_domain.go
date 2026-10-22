package domain

import (
	"strconv"
)

// func KeyUserNameToID(username string) string {
// 	return "un:" + username
// }

func KeyEmailSend(email string) string {
	return "email:" + email + ":send"
}

func KeyEmailToID(email string) string {
	return "email:" + email
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

func RecommendAddress(id uint) string {
	return "user:" + strconv.Itoa(int(id)) + ":recommend:current"
}

func KeyTopicsVec(uid uint) string {
	return "vec:user:" + strconv.Itoa(int(uid))
}

func RecommendCount(uid uint) string {
	return "user:" + strconv.Itoa(int(uid)) + ":recommend:count"
}
// func KeyTopicsVecLock(uid uint) string{
// 	return "vec:user:" + strconv.Itoa(int(uid)) + ":lock"
// }
