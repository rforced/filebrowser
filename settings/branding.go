package settings

type Branding struct {
	Name                  string `json:"name"`
	DisableExternal       bool   `json:"disableExternal"`
	DisableUsedPercentage bool   `json:"disableUsedPercentage"`
	Files                 string `json:"files"`
	Theme                 string `json:"theme"`
	Color                 string `json:"color"`
}
