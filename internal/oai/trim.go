package oai

// TrimMessagesToFit reduces a transcript so its estimated tokens do not exceed
// the provided limit. Policy:
// - Pin the first system and developer messages when present.
// - Drop oldest non-pinned messages first until within limit.
// - If only pinned remain and still exceed limit, truncate their content
//   proportionally but keep both messages.
// - As a last resort, keep only the newest message, truncated to fit.
func TrimMessagesToFit(in []Message, limit int) []Message {
    if limit <= 0 || len(in) == 0 {
        return []Message{}
    }
    estimate := func(msgs []Message) int { return EstimateTokens(msgs) }

    // Fast path: already fits
    if estimate(in) <= limit {
        return in
    }

    out := append([]Message(nil), in...)

    // Drop oldest non-pinned messages until within limit.
    for len(out) > 0 && estimate(out) > limit {
        // Find first indices of pinned roles in current slice
        sysIdx, devIdx := -1, -1
        for i := range out {
            if sysIdx == -1 && out[i].Role == RoleSystem {
                sysIdx = i
            }
            if devIdx == -1 && out[i].Role == RoleDeveloper {
                devIdx = i
            }
            if sysIdx != -1 && devIdx != -1 {
                break
            }
        }
        // Remove first non-pinned from the front if any
        removed := false
        for j := 0; j < len(out); j++ {
            if j != sysIdx && j != devIdx {
                out = append(out[:j], out[j+1:]...)
                removed = true
                break
            }
        }
        if !removed {
            // Only pinned remain; proceed to truncation
            break
        }
    }

    if estimate(out) <= limit {
        return out
    }

    // Truncation path: only pinned remain or still too large
    // Identify pinned indices in current slice
    sysIdx, devIdx := -1, -1
    for i := range out {
        if sysIdx == -1 && out[i].Role == RoleSystem {
            sysIdx = i
        }
        if devIdx == -1 && out[i].Role == RoleDeveloper {
            devIdx = i
        }
    }

    // If no pinned present, keep newest single message truncated to fit
    if sysIdx == -1 && devIdx == -1 {
        last := out[len(out)-1]
        return []Message{truncateMessageToBudget(last, limit)}
    }

    cur := estimate(out)
    if cur <= limit {
        return out
    }

    // Compute budgets
    if sysIdx != -1 && devIdx != -1 {
        sysTok := EstimateTokens([]Message{out[sysIdx]})
        devTok := EstimateTokens([]Message{out[devIdx]})
        totalPinned := sysTok + devTok
        if totalPinned == 0 {
            totalPinned = 1
        }
        nonPinned := cur - totalPinned
        targetPinned := limit - nonPinned
        if targetPinned < 2 { // ensure at least 1 per pinned
            targetPinned = 2
        }
        targetSys := (sysTok * targetPinned) / totalPinned
        if targetSys < 1 {
            targetSys = 1
        }
        targetDev := targetPinned - targetSys
        if targetDev < 1 {
            targetDev = 1
        }
        out[sysIdx] = truncateMessageToBudget(out[sysIdx], targetSys)
        out[devIdx] = truncateMessageToBudget(out[devIdx], targetDev)
    } else if sysIdx != -1 { // only system pinned
        // allocate entire limit minus non-system tokens
        nonSys := cur - EstimateTokens([]Message{out[sysIdx]})
        budget := limit - nonSys
        if budget < 1 {
            budget = 1
        }
        out[sysIdx] = truncateMessageToBudget(out[sysIdx], budget)
    } else if devIdx != -1 { // only developer pinned
        nonDev := cur - EstimateTokens([]Message{out[devIdx]})
        budget := limit - nonDev
        if budget < 1 {
            budget = 1
        }
        out[devIdx] = truncateMessageToBudget(out[devIdx], budget)
    }

    // Final guard: if still above limit, drop oldest non-pinned if any; otherwise truncate newest to fit
    for estimate(out) > limit {
        removed := false
        // Try to remove a non-pinned from the front
        // Recompute pinned indices
        sysIdx, devIdx = -1, -1
        for i := range out {
            if sysIdx == -1 && out[i].Role == RoleSystem {
                sysIdx = i
            }
            if devIdx == -1 && out[i].Role == RoleDeveloper {
                devIdx = i
            }
        }
        for j := 0; j < len(out); j++ {
            if j != sysIdx && j != devIdx {
                out = append(out[:j], out[j+1:]...)
                removed = true
                break
            }
        }
        if !removed {
            // No non-pinned remain; keep newest one truncated
            last := out[len(out)-1]
            out = []Message{truncateMessageToBudget(last, limit)}
            break
        }
    }

    return out
}

// truncateMessageToBudget returns a copy of msg with content truncated such that
// the single-message token estimate is <= budget (best-effort heuristic).
func truncateMessageToBudget(msg Message, budget int) Message {
    if budget <= 1 {
        msg.Content = ""
        return msg
    }
    // Binary search on content length, using EstimateTokens heuristic
    lo, hi := 0, len(msg.Content)
    best := 0
    for lo <= hi {
        mid := (lo + hi) / 2
        test := msg
        test.Content = truncate(msg.Content, mid)
        if EstimateTokens([]Message{test}) <= budget {
            best = mid
            lo = mid + 1
        } else {
            hi = mid - 1
        }
    }
    msg.Content = truncate(msg.Content, best)
    return msg
}
