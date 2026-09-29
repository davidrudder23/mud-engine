// Command demo loads the world and prints it out -- no commands, no
// database; this just proves the JSON -> World pipeline works.
package main

import (
	"fmt"
	"os"

	"mudengine/internal/config"
	"mudengine/internal/world"
)

func main() {
	dir := config.WorldDir()
	w, err := world.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Loaded %d room(s) from %s\n\n", w.Len(), dir)

	for _, room := range w.Rooms() {
		fmt.Printf("[%s] %s\n", room.ID, room.Description)

		if len(room.Items) > 0 {
			fmt.Println("  items:")
			for _, item := range room.Items {
				fmt.Printf("    - %s (%s): %s\n", item.Name, item.ID, item.Description)
			}
		}
		if len(room.Doorways) > 0 {
			fmt.Println("  doorways:")
			for _, d := range room.Doorways {
				fmt.Printf("    - %s -> %s: %s\n", d.Direction, d.To, d.Description)
			}
		}
		fmt.Println()
	}
}
