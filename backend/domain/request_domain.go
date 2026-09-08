package domain

type LoginRequest struct {
	Username string `json:"username" binding:"required,userun"`
	Password string `json:"password" binding:"required,userpwd"`
}

type SignUpRequest struct {
	Username string `json:"username" binding:"required,userun"`
	Password string `json:"password" binding:"required,userpwd"`
	RepeatPassword string `json:"repassword" binding:"required,eqfield=Password"`
}

type UpdateInfoRequest struct {
	Age int `json:"age"`
	Gender string `json:"gender"`
	Job string `json:"job"`
	Education string `json:"education"`
	Location string `json:"location"`
	Organization string `json:"organization"`
	InterestingFields []string `json:"interesting_fields"`
}

type InfoRequest struct {
	Age *int `json:"age" binding:"omitempty,min=0,max=120"`
	Gender *string `json:"gender" binding:"omitempty,oneof=male female"`
	Job *string `json:"job" binding:"omitempty,min=1,max=40"`
	Education *string `json:"education" binding:"omitempty,min=1,max=40"`
	Location *string `json:"location" binding:"omitempty,min=1,max=40"`
	Organization *string `json:"organization" binding:"omitempty,min=1,max=40"`
	InterestingFields *[]string `json:"interesting_fields" binding:"omitempty,min=1,max=10"`
}