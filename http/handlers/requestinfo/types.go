package requestinfo

const (
	UserTypeGuest UserType = "GUEST"
	UserTypeUser  UserType = "USER"
)

type RequestInfo struct {
	UserID    *string
	Purpose   *string
	RawToken  *string
	UserType  *UserType
	RemoteIP  *string
	UserAgent *string
}

type UserType string

func (e UserType) IsValid() bool {
	switch e {
	case UserTypeUser, UserTypeGuest:
		return true
	}

	return false
}

func (e UserType) String() string {
	return string(e)
}
