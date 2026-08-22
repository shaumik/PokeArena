package main

import (
	"fmt"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

func main() {
	d, err := domain.LoadDex("data", "probe")
	if err != nil {
		panic(err)
	}
	for _, n := range []int{3, 36, 38, 47, 49, 53, 65, 68, 76, 82, 94, 95, 97, 105, 107, 113, 119, 122, 127, 131, 137, 143, 146, 150, 151, 6, 59, 20, 26} {
		sp := d.Species[n]
		picks := []engine.TeamPick{{DexNo: n}}
		st, err := engine.NewBattleFromPicks(d, "p", "A", picks, "B", picks, 1)
		if err != nil {
			panic(err)
		}
		m := st.Active(0)
		fmt.Printf("%3d %-12s HP=%3d atk=%3d def=%3d spa=%3d spd=%3d spe=%3d ab=%s\n",
			n, sp.Name, m.MaxHP, m.Stats.Atk, m.Stats.Def, m.Stats.SpA, m.Stats.SpD, m.Stats.Spe, m.Ability)
	}
}
