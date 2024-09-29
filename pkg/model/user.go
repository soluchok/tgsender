package model

type User struct {
	ID         int64  `json:"id"`
	AccessHash int64  `json:"access_hash"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Username   string `json:"username"`
	Phone      string `json:"phone"`
}
