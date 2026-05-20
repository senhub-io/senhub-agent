# Temporary Fork: citrix/adc-nitro-go

**Status**: 🚧 ACTIVE - Using temporary fork
**Date Created**: 2025-12-11
**Created By**: SenHub Development Team
**Review Date**: Every 3 months (next: 2025-03-11)

---

## Summary

We are temporarily using a **forked version** of `github.com/citrix/adc-nitro-go` to fix a critical bug that prevents collecting singleton resource statistics (system, ns, ssl, hanode).

**Our Fork**: https://github.com/senhub-io/adc-nitro-go
**Branch**: `fix/singleton-stats-support`
**Commits**:
- `ee923d74` - Fix FindAllStats() for singleton resources
- `d944ae64` - Fix FindStat() for singleton resources (same issue)

---

## The Problem

The official Citrix ADC NITRO Go SDK has a bug in the `FindAllStats()` function:

- **Issue**: [citrix/adc-nitro-go#35](https://github.com/citrix/adc-nitro-go/issues/35)
- **Opened**: August 27, 2022
- **Status**: ❌ Still open (3+ years)

### Technical Details

The `FindAllStats()` function assumes API responses are always arrays:
```go
resources := data[resourceType].([]interface{})
```

However, **singleton resources** (system, ns, ssl, hanode) return a **single object** instead:
```go
// Returns this:
{"system": {...}}  // map[string]interface{}

// Not this:
{"lbvserver": [{...}, {...}]}  // []interface{}
```

This causes a **runtime panic**:
```
panic: interface conversion: interface {} is map[string]interface {}, not []interface {}
```

---

## Our Fix

We implemented the fix proposed in [PR #36](https://github.com/citrix/adc-nitro-go/pull/36) (also 3 years old, never merged), **extended to both functions**:

### Fix 1: FindAllStats() (from PR #36)

```go
// Handle both array and singleton responses
switch result := data[resourceType].(type) {
case []interface{}:
    // Array response - multiple resources (e.g., lbvserver, service)
    ret := make([]map[string]interface{}, len(result))
    for i, v := range result {
        ret[i] = v.(map[string]interface{})
    }
    return ret, nil
case map[string]interface{}:
    // Singleton response - single resource (e.g., system, ns, ssl)
    return []map[string]interface{}{result}, nil
default:
    return nil, fmt.Errorf("unexpected response type")
}
```

### Fix 2: FindStat() (our additional fix)

**Same bug affected FindStat()** when called with empty resourceName (singleton resources):

```go
// Handle both array and singleton responses
switch res := data[resourceType].(type) {
case []interface{}:
    // Array response - return first element
    if len(res) > 0 {
        return res[0].(map[string]interface{}), nil
    }
    return nil, fmt.Errorf("empty array")
case map[string]interface{}:
    // Singleton response - return directly
    return res, nil
default:
    return nil, fmt.Errorf("unexpected response type")
}
```

**Files Modified**: `service/stats.go`
**Functions**: `FindAllStats()` and `FindStat()`

---

## Impact

### Before Fix (Disabled Metrics)
```yaml
# These were commented out due to the bug:
- System CPU/Memory usage
- System Network throughput
- Global NS throughput
- SSL transaction rates
```

### After Fix (All Metrics Working)
```yaml
✅ System-level metrics (CPU, Memory, Network)
✅ NS global metrics (Throughput)
✅ SSL metrics (Transactions, Sessions)
✅ LB VServer metrics
✅ Service metrics
✅ ServiceGroup metrics
```

---

## How to Revert to Official SDK

When Citrix merges the fix, follow these steps:

### 1. Check if Fix is Merged
```bash
# Check upstream PR status
curl -s https://api.github.com/repos/citrix/adc-nitro-go/pulls/36 | jq '.merged'

# Or check latest commits
git clone https://github.com/citrix/adc-nitro-go.git /tmp/citrix-check
cd /tmp/citrix-check
git log --oneline --grep="singleton\|FindAllStats" -i
```

### 2. Test Official SDK
```bash
# Remove our fork
cd /Users/matthieu/Documents/GitHub/senhub-agent
git checkout go.mod go.sum

# Get latest official SDK
go get -u github.com/citrix/adc-nitro-go@latest
go mod tidy

# Test with netscaler probe
make build-darwin
./dist/senhub-agent run --verbose
```

### 3. Update go.mod
Remove the `replace` directive from `go.mod`:
```diff
- // TEMPORARY FORK: Using senhub-io fork with singleton stats fix
- // This can be removed once upstream merges PR #36
- // Upstream PR: https://github.com/citrix/adc-nitro-go/pull/36
- // Our fix: https://github.com/senhub-io/adc-nitro-go/commit/ee923d74da8155d8caec51efdd3739116cb62f81
- replace github.com/citrix/adc-nitro-go => github.com/senhub-io/adc-nitro-go v0.0.0-20251211095156-ee923d74da81
```

### 4. Verify All Metrics Work
```bash
# Check PRTG endpoint includes system/ns/ssl metrics
curl http://localhost:8080/api/{key}/prtg/metrics/netscaler-adc | jq '.prtg.result[] | select(.channel | contains("System") or contains("NS") or contains("SSL"))'
```

### 5. Update Documentation
- Delete this file: `docs/.internal/TEMPORARY-FORK-citrix-adc-nitro-go.md`
- Update release notes mentioning the revert
- Update `CLAUDE.md` if it references the fork

---

## Maintenance

### Quarterly Review Checklist

Every 3 months, check:

- [ ] Has [PR #36](https://github.com/citrix/adc-nitro-go/pull/36) been merged?
- [ ] Are there new releases of the official SDK?
- [ ] Does our fork need to be rebased on latest upstream?
- [ ] Are all tests still passing with our fork?
- [ ] Update "Review Date" in this document

### Keeping Fork Up-to-Date

If upstream adds features we need:
```bash
cd /tmp
git clone https://github.com/senhub-io/adc-nitro-go.git
cd adc-nitro-go
git remote add upstream https://github.com/citrix/adc-nitro-go.git
git fetch upstream
git checkout fix/singleton-stats-support
git rebase upstream/main
git push -f origin fix/singleton-stats-support

# Update go.mod with new commit hash
cd /Users/matthieu/Documents/GitHub/senhub-agent
go get github.com/senhub-io/adc-nitro-go@latest
go mod tidy
```

---

## References

- **Upstream Issue**: https://github.com/citrix/adc-nitro-go/issues/35
- **Upstream PR** (never merged): https://github.com/citrix/adc-nitro-go/pull/36
- **Our Fork**: https://github.com/senhub-io/adc-nitro-go
- **Our Fix Commit**: https://github.com/senhub-io/adc-nitro-go/commit/ee923d74da8155d8caec51efdd3739116cb62f81
- **Related Files**:
  - `go.mod` (line 57): Replace directive
  - `internal/agent/probes/netscaler/netscaler_probe.go`: Netscaler probe
  - `internal/agent/services/data_store/transformers/definitions/netscaler.yaml`: Metric definitions

---

## Contact

If you have questions about this fork:
- **Technical Lead**: SenHub Development Team
- **Issue Tracking**: GitHub Issues on senhub-agent repository
- **Review Process**: Quarterly review in sprint planning

---

**Last Updated**: 2025-12-11
**Next Review**: 2025-03-11
