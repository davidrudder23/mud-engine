package world

// Doorway is a connection from one room to another.
type Doorway struct {
	Direction   string
	To          string // room id of the destination
	Description string
}

func (d *Doorway) Format() string {
	if d.Description != "" {
		return d.Direction + " (" + d.Description + ")"
	}
	return d.Direction
}

// DoorwayView is the JSON-serializable snapshot of a Doorway.
type DoorwayView struct {
	Direction   string `json:"direction"`
	To          string `json:"to"`
	Description string `json:"description"`
}

func (d *Doorway) View() DoorwayView {
	return DoorwayView{Direction: d.Direction, To: d.To, Description: d.Description}
}
