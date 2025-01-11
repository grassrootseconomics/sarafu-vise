package profile

type Profile struct {
	ProfileItems []string
	Max          int
}

func (p *Profile) InsertOrShift(index int, value string) {
	if index < len(p.ProfileItems) {
		p.ProfileItems = append(p.ProfileItems[:index], value)
	} else {
		for len(p.ProfileItems) < index {
			p.ProfileItems = append(p.ProfileItems, "0")
		}
		p.ProfileItems = append(p.ProfileItems, "0") 
		p.ProfileItems[index] = value
	}
}
