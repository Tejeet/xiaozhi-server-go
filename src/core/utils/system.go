package utils

import (
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// GetSystemMemoryUsage gets the current system memory usage percentage (cross-platform)
func GetSystemMemoryUsage() (float64, error) {
	vmStat, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return vmStat.UsedPercent, nil
}

// GetSystemCPUUsage gets the current CPU usage percentage (cross-platform, averaged over all cores)
func GetSystemCPUUsage() (float64, error) {
	percentages, err := cpu.Percent(0, false)
	if err != nil {
		return 0, err
	}
	if len(percentages) == 0 {
		return 0, nil
	}
	return percentages[0], nil
}
