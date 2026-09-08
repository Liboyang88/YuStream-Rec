package domain

import (
	"encoding/json"

	"github.com/hibiken/asynq"
)

const (
	TypeAddNewUser     = "user:add"
	TypeAddNewInteraction = "interaction:add"
	TypeUpdateUserInfo = "user_info:update"
	TypeGenerateUS     = "user_summary:generate"
	TypeUpdateUS       = "user_summary:update"
)

type AddNewUserPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AddNewInteractionPayload struct {
	UserID uint `json:"user_id"`
	Payload string `json:"payload"`
}

type UpdateUserInfoPayload struct {
	ID           uint                   `json:"id"`
	UpdateFields map[string]interface{} `json:"update_fields"`
}

type GenerateUSPayload struct {
	UserID uint `json:"user_id"`
}

type UpdateUSPayload struct {
	UserID uint `json:"user_id"`
}

func NewAddNewUserTask(username string, password string) (*asynq.Task ,error) {
	payload, err := json.Marshal(AddNewUserPayload{
		Username: username, 
		Password: password})

	if err != nil{ 
		return nil, err
	}

	return asynq.NewTask(TypeAddNewUser, payload), nil
}

func NewAddNewInteractionTask(uid uint, str string) (*asynq.Task ,error) { 
	payload, err := json.Marshal(AddNewInteractionPayload{
		UserID: uid, 
		Payload: str})
	if err != nil{ 
		return nil, err
	}
	return asynq.NewTask(TypeAddNewInteraction, payload), nil
}

func NewUpdateUserInfoTask(id uint, updateFields map[string]interface{}) (*asynq.Task ,error) { 
	payload, err := json.Marshal(UpdateUserInfoPayload{
		ID: id, 
		UpdateFields: updateFields})

	if err != nil{ 
		return nil, err
	}

	return asynq.NewTask(TypeUpdateUserInfo, payload), nil
}

func NewGenerateUSTask(id uint) (*asynq.Task ,error) { 
	payload, err := json.Marshal(GenerateUSPayload{
		UserID: id})
	if err != nil{ 
		return nil, err
	}

	return asynq.NewTask(TypeGenerateUS, payload), nil
}

func NewUpdateUSTask(userID uint) (*asynq.Task ,error) { 
	payload, err := json.Marshal(UpdateUSPayload{
		UserID: userID})
	if err != nil{ 
		return nil, err
	}
	
	return asynq.NewTask(TypeUpdateUS, payload), nil
}