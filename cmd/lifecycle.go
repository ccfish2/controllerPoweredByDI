package cmd

import "github.com/ccfish2/infra/pkg/hive/cell"

type LeaderLifecycle struct {
	cell.DefaultLifecycle
}

func WithLeaderLifecycle(cells ...cell.Cell) cell.Cell {
	return cell.Module(
		"leader-lifecycle",
		"Operator Leader Lifecycle",
		cell.Provide(func() *LeaderLifecycle { return &LeaderLifecycle{} }),
		cell.Decorate(func(lc *LeaderLifecycle) cell.Lifecycle {
			return lc
		},
			cells...),
	)
}
