// internal/agent/windows/pdh/pdh.go
//go:build windows

package pdh

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"strings"
	"sync"
	"unsafe"

	"senhub-agent.go/internal/agent/services/logger"
)

var (
	// moduleLogger for PDH operations - initialized with basic logger
	moduleLogger *logger.ModuleLogger
)

// ErrNoData reports a PDH_NO_DATA condition. It is expected on the first
// sample of a rate counter — PDH has no delta until a second sample exists —
// so callers that prime then re-collect should tolerate it via errors.Is
// rather than treating it as a hard failure.
var ErrNoData = errors.New("no PDH sample available yet")

// InitializePDHLogger initializes the PDH module logger
func InitializePDHLogger(baseLogger *logger.Logger) {
	moduleLogger = logger.NewModuleLogger(baseLogger, "pdh.windows")
}

// logDebug safely logs debug messages, falling back to no-op if logger not initialized
func logDebug(msg string, args ...interface{}) {
	if moduleLogger != nil {
		if len(args) > 0 {
			moduleLogger.Debug().Msgf(msg, args...)
		} else {
			moduleLogger.Debug().Msg(msg)
		}
	}
	// If moduleLogger is nil, do nothing (silent fallback)
}

const (
	PDH_CSTATUS_VALID_DATA                     = 0x00000000
	PDH_CSTATUS_NEW_DATA                       = 0x00000001
	PDH_CSTATUS_NO_MACHINE                     = 0x800007D0
	PDH_CSTATUS_NO_INSTANCE                    = 0x800007D1
	PDH_MORE_DATA                              = 0x800007D2
	PDH_CSTATUS_ITEM_NOT_VALIDATED             = 0x800007D3
	PDH_RETRY                                  = 0x800007D4
	PDH_NO_DATA                                = 0x800007D5
	PDH_CALC_NEGATIVE_DENOMINATOR              = 0x800007D6
	PDH_CALC_NEGATIVE_TIMEBASE                 = 0x800007D7
	PDH_CALC_NEGATIVE_VALUE                    = 0x800007D8
	PDH_DIALOG_CANCELLED                       = 0x800007D9
	PDH_END_OF_LOG_FILE                        = 0x800007DA
	PDH_ASYNC_QUERY_TIMEOUT                    = 0x800007DB
	PDH_CANNOT_SET_DEFAULT_REALTIME_DATASOURCE = 0x800007DC
	PDH_CSTATUS_NO_OBJECT                      = 0xC0000BB8
	PDH_CSTATUS_NO_COUNTER                     = 0xC0000BB9
	PDH_CSTATUS_INVALID_DATA                   = 0xC0000BBA
	PDH_MEMORY_ALLOCATION_FAILURE              = 0xC0000BBB
	PDH_INVALID_HANDLE                         = 0xC0000BBC
	PDH_INVALID_ARGUMENT                       = 0xC0000BBD
	PDH_FUNCTION_NOT_FOUND                     = 0xC0000BBE
	PDH_CSTATUS_NO_COUNTERNAME                 = 0xC0000BBF
	PDH_CSTATUS_BAD_COUNTERNAME                = 0xC0000BC0
	PDH_INVALID_BUFFER                         = 0xC0000BC1
	PDH_INSUFFICIENT_BUFFER                    = 0xC0000BC2
	PDH_CANNOT_CONNECT_MACHINE                 = 0xC0000BC3
	PDH_INVALID_PATH                           = 0xC0000BC4
	PDH_INVALID_INSTANCE                       = 0xC0000BC5
	PDH_INVALID_DATA                           = 0xC0000BC6
	PDH_NO_DIALOG_DATA                         = 0xC0000BC7
	PDH_CANNOT_READ_NAME_STRINGS               = 0xC0000BC8
	PDH_LOG_FILE_CREATE_ERROR                  = 0xC0000BC9
	PDH_LOG_FILE_OPEN_ERROR                    = 0xC0000BCA
	PDH_LOG_TYPE_NOT_FOUND                     = 0xC0000BCB
	PDH_NO_MORE_DATA                           = 0xC0000BCC
	PDH_ENTRY_NOT_IN_LOG_FILE                  = 0xC0000BCD
	PDH_DATA_SOURCE_IS_LOG_FILE                = 0xC0000BCE
	PDH_DATA_SOURCE_IS_REAL_TIME               = 0xC0000BCF
	PDH_UNABLE_READ_LOG_HEADER                 = 0xC0000BD0
	PDH_FILE_NOT_FOUND                         = 0xC0000BD1
	PDH_FILE_ALREADY_EXISTS                    = 0xC0000BD2
	PDH_NOT_IMPLEMENTED                        = 0xC0000BD3
	PDH_STRING_NOT_FOUND                       = 0xC0000BD4
	PDH_UNABLE_MAP_NAME_FILES                  = 0x80000BD5
	PDH_UNKNOWN_LOG_FORMAT                     = 0xC0000BD6
	PDH_UNKNOWN_LOGSVC_COMMAND                 = 0xC0000BD7
	PDH_LOGSVC_QUERY_NOT_FOUND                 = 0xC0000BD8
	PDH_LOGSVC_NOT_OPENED                      = 0xC0000BD9
	PDH_WBEM_ERROR                             = 0xC0000BDA
	PDH_ACCESS_DENIED                          = 0xC0000BDB
	PDH_LOG_FILE_TOO_SMALL                     = 0xC0000BDC
	PDH_INVALID_DATASOURCE                     = 0xC0000BDD
	PDH_INVALID_SQLDB                          = 0xC0000BDE
	PDH_NO_COUNTERS                            = 0xC0000BDF
	PDH_SQL_ALLOC_FAILED                       = 0xC0000BE0
	PDH_SQL_ALLOCCON_FAILED                    = 0xC0000BE1
	PDH_SQL_EXEC_DIRECT_FAILED                 = 0xC0000BE2
	PDH_SQL_FETCH_FAILED                       = 0xC0000BE3
	PDH_SQL_ROWCOUNT_FAILED                    = 0xC0000BE4
	PDH_SQL_MORE_RESULTS_FAILED                = 0xC0000BE5
	PDH_SQL_CONNECT_FAILED                     = 0xC0000BE6
	PDH_SQL_BIND_FAILED                        = 0xC0000BE7
	PDH_CANNOT_CONNECT_WMI_SERVER              = 0xC0000BE8
	PDH_PLA_COLLECTION_ALREADY_RUNNING         = 0xC0000BE9
	PDH_PLA_ERROR_SCHEDULE_OVERLAP             = 0xC0000BEA
	PDH_PLA_COLLECTION_NOT_FOUND               = 0xC0000BEB
	PDH_PLA_ERROR_SCHEDULE_ELAPSED             = 0xC0000BEC
	PDH_PLA_ERROR_NOSTART                      = 0xC0000BED
	PDH_PLA_ERROR_ALREADY_EXISTS               = 0xC0000BEE
	PDH_PLA_ERROR_TYPE_MISMATCH                = 0xC0000BEF
	PDH_PLA_ERROR_FILEPATH                     = 0xC0000BF0
	PDH_PLA_SERVICE_ERROR                      = 0xC0000BF1
	PDH_PLA_VALIDATION_ERROR                   = 0xC0000BF2
	PDH_PLA_VALIDATION_WARNING                 = 0x80000BF3
	PDH_PLA_ERROR_NAME_TOO_LONG                = 0xC0000BF4
	PDH_INVALID_SQL_LOG_FORMAT                 = 0xC0000BF5
	PDH_COUNTER_ALREADY_IN_QUERY               = 0xC0000BF6
	PDH_BINARY_LOG_CORRUPT                     = 0xC0000BF7
	PDH_LOG_SAMPLE_TOO_SMALL                   = 0xC0000BF8
	PDH_OS_LATER_VERSION                       = 0xC0000BF9
	PDH_OS_EARLIER_VERSION                     = 0xC0000BFA
	PDH_INCORRECT_APPEND_TIME                  = 0xC0000BFB
	PDH_UNMATCHED_APPEND_COUNTER               = 0xC0000BFC
	PDH_SQL_ALTER_DETAIL_FAILED                = 0xC0000BFD
	PDH_QUERY_PERF_DATA_TIMEOUT                = 0xC0000BFE
)

const (
	PDH_FMT_RAW          = 0x00000010
	PDH_FMT_ANSI         = 0x00000020
	PDH_FMT_UNICODE      = 0x00000040
	PDH_FMT_LONG         = 0x00000100
	PDH_FMT_DOUBLE       = 0x00000200
	PDH_FMT_LARGE        = 0x00000400
	PDH_FMT_NOSCALE      = 0x00001000
	PDH_FMT_1000         = 0x00002000
	PDH_FMT_NODATA       = 0x00004000
	PDH_FMT_NOCAP100     = 0x00008000
	PERF_DETAIL_COSTLY   = 0x00010000
	PERF_DETAIL_STANDARD = 0x0000FFFF
)

var pdhErrors = map[uint32]string{
	PDH_CSTATUS_VALID_DATA:                     "PDH_CSTATUS_VALID_DATA",
	PDH_CSTATUS_NEW_DATA:                       "PDH_CSTATUS_NEW_DATA",
	PDH_CSTATUS_NO_MACHINE:                     "PDH_CSTATUS_NO_MACHINE",
	PDH_CSTATUS_NO_INSTANCE:                    "PDH_CSTATUS_NO_INSTANCE",
	PDH_MORE_DATA:                              "PDH_MORE_DATA",
	PDH_CSTATUS_ITEM_NOT_VALIDATED:             "PDH_CSTATUS_ITEM_NOT_VALIDATED",
	PDH_RETRY:                                  "PDH_RETRY",
	PDH_NO_DATA:                                "PDH_NO_DATA",
	PDH_CALC_NEGATIVE_DENOMINATOR:              "PDH_CALC_NEGATIVE_DENOMINATOR",
	PDH_CALC_NEGATIVE_TIMEBASE:                 "PDH_CALC_NEGATIVE_TIMEBASE",
	PDH_CALC_NEGATIVE_VALUE:                    "PDH_CALC_NEGATIVE_VALUE",
	PDH_DIALOG_CANCELLED:                       "PDH_DIALOG_CANCELLED",
	PDH_END_OF_LOG_FILE:                        "PDH_END_OF_LOG_FILE",
	PDH_ASYNC_QUERY_TIMEOUT:                    "PDH_ASYNC_QUERY_TIMEOUT",
	PDH_CANNOT_SET_DEFAULT_REALTIME_DATASOURCE: "PDH_CANNOT_SET_DEFAULT_REALTIME_DATASOURCE",
	PDH_CSTATUS_NO_OBJECT:                      "PDH_CSTATUS_NO_OBJECT",
	PDH_CSTATUS_NO_COUNTER:                     "PDH_CSTATUS_NO_COUNTER",
	PDH_CSTATUS_INVALID_DATA:                   "PDH_CSTATUS_INVALID_DATA",
	PDH_MEMORY_ALLOCATION_FAILURE:              "PDH_MEMORY_ALLOCATION_FAILURE",
	PDH_INVALID_HANDLE:                         "PDH_INVALID_HANDLE",
	PDH_INVALID_ARGUMENT:                       "PDH_INVALID_ARGUMENT",
	PDH_FUNCTION_NOT_FOUND:                     "PDH_FUNCTION_NOT_FOUND",
	PDH_CSTATUS_NO_COUNTERNAME:                 "PDH_CSTATUS_NO_COUNTERNAME",
	PDH_CSTATUS_BAD_COUNTERNAME:                "PDH_CSTATUS_BAD_COUNTERNAME",
	PDH_INVALID_BUFFER:                         "PDH_INVALID_BUFFER",
	PDH_INSUFFICIENT_BUFFER:                    "PDH_INSUFFICIENT_BUFFER",
	PDH_CANNOT_CONNECT_MACHINE:                 "PDH_CANNOT_CONNECT_MACHINE",
	PDH_INVALID_PATH:                           "PDH_INVALID_PATH",
	PDH_INVALID_INSTANCE:                       "PDH_INVALID_INSTANCE",
	PDH_INVALID_DATA:                           "PDH_INVALID_DATA",
	PDH_NO_DIALOG_DATA:                         "PDH_NO_DIALOG_DATA",
	PDH_CANNOT_READ_NAME_STRINGS:               "PDH_CANNOT_READ_NAME_STRINGS",
	PDH_LOG_FILE_CREATE_ERROR:                  "PDH_LOG_FILE_CREATE_ERROR",
	PDH_LOG_FILE_OPEN_ERROR:                    "PDH_LOG_FILE_OPEN_ERROR",
	PDH_LOG_TYPE_NOT_FOUND:                     "PDH_LOG_TYPE_NOT_FOUND",
	PDH_NO_MORE_DATA:                           "PDH_NO_MORE_DATA",
	PDH_ENTRY_NOT_IN_LOG_FILE:                  "PDH_ENTRY_NOT_IN_LOG_FILE",
	PDH_DATA_SOURCE_IS_LOG_FILE:                "PDH_DATA_SOURCE_IS_LOG_FILE",
	PDH_DATA_SOURCE_IS_REAL_TIME:               "PDH_DATA_SOURCE_IS_REAL_TIME",
	PDH_UNABLE_READ_LOG_HEADER:                 "PDH_UNABLE_READ_LOG_HEADER",
	PDH_FILE_NOT_FOUND:                         "PDH_FILE_NOT_FOUND",
	PDH_FILE_ALREADY_EXISTS:                    "PDH_FILE_ALREADY_EXISTS",
	PDH_NOT_IMPLEMENTED:                        "PDH_NOT_IMPLEMENTED",
	PDH_STRING_NOT_FOUND:                       "PDH_STRING_NOT_FOUND",
	PDH_UNABLE_MAP_NAME_FILES:                  "PDH_UNABLE_MAP_NAME_FILES",
	PDH_UNKNOWN_LOG_FORMAT:                     "PDH_UNKNOWN_LOG_FORMAT",
	PDH_UNKNOWN_LOGSVC_COMMAND:                 "PDH_UNKNOWN_LOGSVC_COMMAND",
	PDH_LOGSVC_QUERY_NOT_FOUND:                 "PDH_LOGSVC_QUERY_NOT_FOUND",
	PDH_LOGSVC_NOT_OPENED:                      "PDH_LOGSVC_NOT_OPENED",
	PDH_WBEM_ERROR:                             "PDH_WBEM_ERROR",
	PDH_ACCESS_DENIED:                          "PDH_ACCESS_DENIED",
	PDH_LOG_FILE_TOO_SMALL:                     "PDH_LOG_FILE_TOO_SMALL",
	PDH_INVALID_DATASOURCE:                     "PDH_INVALID_DATASOURCE",
	PDH_INVALID_SQLDB:                          "PDH_INVALID_SQLDB",
	PDH_NO_COUNTERS:                            "PDH_NO_COUNTERS",
	PDH_SQL_ALLOC_FAILED:                       "PDH_SQL_ALLOC_FAILED",
	PDH_SQL_ALLOCCON_FAILED:                    "PDH_SQL_ALLOCCON_FAILED",
	PDH_SQL_EXEC_DIRECT_FAILED:                 "PDH_SQL_EXEC_DIRECT_FAILED",
	PDH_SQL_FETCH_FAILED:                       "PDH_SQL_FETCH_FAILED",
	PDH_SQL_ROWCOUNT_FAILED:                    "PDH_SQL_ROWCOUNT_FAILED",
	PDH_SQL_MORE_RESULTS_FAILED:                "PDH_SQL_MORE_RESULTS_FAILED",
	PDH_SQL_CONNECT_FAILED:                     "PDH_SQL_CONNECT_FAILED",
	PDH_SQL_BIND_FAILED:                        "PDH_SQL_BIND_FAILED",
	PDH_CANNOT_CONNECT_WMI_SERVER:              "PDH_CANNOT_CONNECT_WMI_SERVER",
	PDH_PLA_COLLECTION_ALREADY_RUNNING:         "PDH_PLA_COLLECTION_ALREADY_RUNNING",
	PDH_PLA_ERROR_SCHEDULE_OVERLAP:             "PDH_PLA_ERROR_SCHEDULE_OVERLAP",
	PDH_PLA_COLLECTION_NOT_FOUND:               "PDH_PLA_COLLECTION_NOT_FOUND",
	PDH_PLA_ERROR_SCHEDULE_ELAPSED:             "PDH_PLA_ERROR_SCHEDULE_ELAPSED",
	PDH_PLA_ERROR_NOSTART:                      "PDH_PLA_ERROR_NOSTART",
	PDH_PLA_ERROR_ALREADY_EXISTS:               "PDH_PLA_ERROR_ALREADY_EXISTS",
	PDH_PLA_ERROR_TYPE_MISMATCH:                "PDH_PLA_ERROR_TYPE_MISMATCH",
	PDH_PLA_ERROR_FILEPATH:                     "PDH_PLA_ERROR_FILEPATH",
	PDH_PLA_SERVICE_ERROR:                      "PDH_PLA_SERVICE_ERROR",
	PDH_PLA_VALIDATION_ERROR:                   "PDH_PLA_VALIDATION_ERROR",
	PDH_PLA_VALIDATION_WARNING:                 "PDH_PLA_VALIDATION_WARNING",
	PDH_PLA_ERROR_NAME_TOO_LONG:                "PDH_PLA_ERROR_NAME_TOO_LONG",
	PDH_INVALID_SQL_LOG_FORMAT:                 "PDH_INVALID_SQL_LOG_FORMAT",
	PDH_COUNTER_ALREADY_IN_QUERY:               "PDH_COUNTER_ALREADY_IN_QUERY",
	PDH_BINARY_LOG_CORRUPT:                     "PDH_BINARY_LOG_CORRUPT",
	PDH_LOG_SAMPLE_TOO_SMALL:                   "PDH_LOG_SAMPLE_TOO_SMALL",
	PDH_OS_LATER_VERSION:                       "PDH_OS_LATER_VERSION",
	PDH_OS_EARLIER_VERSION:                     "PDH_OS_EARLIER_VERSION",
	PDH_INCORRECT_APPEND_TIME:                  "PDH_INCORRECT_APPEND_TIME",
	PDH_UNMATCHED_APPEND_COUNTER:               "PDH_UNMATCHED_APPEND_COUNTER",
	PDH_SQL_ALTER_DETAIL_FAILED:                "PDH_SQL_ALTER_DETAIL_FAILED",
	PDH_QUERY_PERF_DATA_TIMEOUT:                "PDH_QUERY_PERF_DATA_TIMEOUT",
}

var (
	modpdh                      = windows.NewLazySystemDLL("pdh.dll")
	pdhOpenQuery                = modpdh.NewProc("PdhOpenQueryW")
	pdhAddEnglishCounterA       = modpdh.NewProc("PdhAddEnglishCounterA")
	pdhCollectQueryData         = modpdh.NewProc("PdhCollectQueryData")
	pdhGetFormattedCounterValue = modpdh.NewProc("PdhGetFormattedCounterValue")
	pdhEnumObjectItemsW         = modpdh.NewProc("PdhEnumObjectItemsW")
	pdhLookupPerfNameByIndexW   = modpdh.NewProc("PdhLookupPerfNameByIndexW")
	pdhCloseQuery               = modpdh.NewProc("PdhCloseQuery")
)

// PerfCounterIndex contient la correspondance entre les noms anglais des compteurs de performance
// et leurs indices dans le système Windows
var PerfCounterIndexes = map[string][]uint32{
	// Disques
	"LogicalDisk":  {236, 1847, 830, 234},
	"PhysicalDisk": {234, 1846, 828, 236},
	"HardDisk":     {234, 1846, 828, 236},

	// CPU
	"Processor":             {238, 1848, 238, 732, 1732},
	"Processor Information": {238, 1848, 238, 732, 1732},

	// Réseau
	"Network Interface": {510, 1847, 1450, 3576},
	"Network Adapter":   {442, 1856, 1380, 3450},
	"TCPv4":             {638, 1957, 1480, 3726},
	"TCPv6":             {639, 1958, 1482, 3728},
	"IPv4":              {636, 1955, 1476, 3724},
	"IPv6":              {637, 1956, 1478, 3725},
	"NBT Connection":    {502, 1843, 1412, 3542},

	// Mémoire et Paging
	"Memory":         {4, 1884, 804, 2600},
	"PagingFile":     {144, 1946, 1866, 864},
	"Cache":          {86, 1922, 822, 2618},
	"Pool":           {106, 1926, 826, 2622},
	"VirtualMemory":  {144, 1946, 1866, 864},
	"PageFile":       {144, 1946, 1866, 864},
	"Process Memory": {186, 1986, 1906, 904},

	// Système
	"System":             {2, 1882, 802, 2598},
	"Process":            {230, 1842, 824, 2620},
	"Thread":             {232, 1844, 826, 2622},
	"Objects":            {260, 1872, 854, 2650},
	"Services":           {388, 1898, 880, 2676},
	"Server":             {330, 1888, 870, 2666},
	"Server Work Queues": {332, 1890, 872, 2668},

	// USB
	"USB": {404, 1904, 886, 2682},

	// Base de données
	"Database": {445, 1858, 840, 2636},
	"MSSQL":    {446, 1860, 842, 2638},

	// Web et .NET
	"Web Service": {340, 1892, 874, 2670},
	".NET CLR":    {582, 1908, 890, 2686},
	"ASP.NET":     {576, 1902, 884, 2680},

	// Terminal Services
	"Terminal Services": {422, 1850, 832, 2628},
	"Remote Desktop":    {424, 1852, 834, 2630},
}

type PDH_FMT_COUNTERVALUE struct {
	CStatus uint32
	Value   float64
}

type PDH_HQUERY windows.Handle
type PDH_HCOUNTER windows.Handle

type Query struct {
	handle   PDH_HQUERY
	counters map[string]PDH_HCOUNTER
	mutex    sync.Mutex
}

func GetPdhErrorText(errorCode uint32) string {
	if text, exists := pdhErrors[errorCode]; exists {
		return text
	}
	return fmt.Sprintf("Unknown error code: 0x%X", errorCode)
}

func GetLocalizedCounterName(englishName string) (string, error) {
	indexes, exists := PerfCounterIndexes[englishName]
	if !exists {
		return "", fmt.Errorf("unknown performance object name: %s", englishName)
	}

	var lastErr error
	for _, index := range indexes {
		var bufferSize uint32 = 0
		ret, _, _ := pdhLookupPerfNameByIndexW.Call(
			0,
			uintptr(index),
			0,
			uintptr(unsafe.Pointer(&bufferSize)))

		if ret != PDH_MORE_DATA {
			lastErr = fmt.Errorf("failed to get buffer size for index %d: %s", index, GetPdhErrorText(uint32(ret)))
			continue
		}

		buffer := make([]uint16, bufferSize)
		ret, _, _ = pdhLookupPerfNameByIndexW.Call(
			0,
			uintptr(index),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(unsafe.Pointer(&bufferSize)))

		if ret == PDH_CSTATUS_VALID_DATA {
			return windows.UTF16ToString(buffer), nil
		}
		lastErr = fmt.Errorf("failed to get localized name for index %d: %s", index, GetPdhErrorText(uint32(ret)))
	}

	return "", fmt.Errorf("all indexes failed for %s: %v", englishName, lastErr)
}

func GetInstancesList(objectName string, debug bool) ([]string, error) {
	if debug {
		logDebug("GetInstancesList: Starting enumeration for object '%s'", objectName)
	}

	localizedName, err := GetLocalizedCounterName(objectName)
	if err != nil {
		if debug {
			logDebug("GetInstancesList: Failed to get localized name: %v", err)
		}
		return nil, fmt.Errorf("failed to get localized name: %v", err)
	}

	if debug {
		logDebug("GetInstancesList: Got localized name '%s' for '%s'", localizedName, objectName)
	}

	objectNameUTF16, err := windows.UTF16PtrFromString(localizedName)
	if err != nil {
		return nil, fmt.Errorf("failed to convert name to UTF16: %v", err)
	}

	var counterSize, instanceSize uint32
	var detailLevel uint32 = PERF_DETAIL_STANDARD

	// Premier appel pour obtenir les tailles
	ret, _, _ := pdhEnumObjectItemsW.Call(
		0, // NULL machine name
		0, // NULL DataSource
		uintptr(unsafe.Pointer(objectNameUTF16)),
		0, // NULL CounterList
		uintptr(unsafe.Pointer(&counterSize)),
		0, // NULL InstanceList
		uintptr(unsafe.Pointer(&instanceSize)),
		uintptr(detailLevel),
		0)

	if debug {
		logDebug("GetInstancesList: First call returned 0x%X, counter size: %d, instance size: %d",
			ret, counterSize, instanceSize)
	}

	// Allouer les buffers
	counterList := make([]uint16, counterSize)
	instanceList := make([]uint16, instanceSize)

	// Deuxième appel pour obtenir les données
	ret, _, _ = pdhEnumObjectItemsW.Call(
		0, // NULL machine name
		0, // NULL DataSource
		uintptr(unsafe.Pointer(objectNameUTF16)),
		uintptr(unsafe.Pointer(&counterList[0])),
		uintptr(unsafe.Pointer(&counterSize)),
		uintptr(unsafe.Pointer(&instanceList[0])),
		uintptr(unsafe.Pointer(&instanceSize)),
		uintptr(detailLevel),
		0)

	if debug {
		logDebug("GetInstancesList: Second call returned 0x%X", ret)
	}

	if ret == PDH_CSTATUS_VALID_DATA {
		var instances []string
		var currentInstance []uint16

		// Parcourir le buffer d'instances
		for _, char := range instanceList {
			if char == 0 {
				if len(currentInstance) > 0 {
					instance := windows.UTF16ToString(currentInstance)
					if instance != "" && instance != "_Total" {
						if debug {
							logDebug("GetInstancesList: Found instance: '%s'", instance)
						}
						instances = append(instances, instance)
					}
				}
				currentInstance = []uint16{}
			} else {
				currentInstance = append(currentInstance, char)
			}
		}

		if len(instances) > 0 {
			return instances, nil
		}
	}

	if debug {
		logDebug("GetInstancesList: No valid instances found, error code: 0x%X", ret)
	}
	return nil, fmt.Errorf("no valid instances found (error code: 0x%X)", ret)
}

func NewQuery() (*Query, error) {
	var handle PDH_HQUERY
	ret, _, _ := pdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&handle)))
	if ret != 0 {
		return nil, fmt.Errorf("PdhOpenQuery error: %s (0x%X)", GetPdhErrorText(uint32(ret)), ret)
	}

	return &Query{
		handle:   handle,
		counters: make(map[string]PDH_HCOUNTER),
	}, nil
}

func (q *Query) AddCounter(path string) error {
	logDebug("Adding counter with path: %s", path)
	q.mutex.Lock()
	defer q.mutex.Unlock()

	var counter PDH_HCOUNTER
	ret, _, _ := pdhAddEnglishCounterA.Call(
		uintptr(q.handle),
		uintptr(unsafe.Pointer(&([]byte(path + "\x00")[0]))),
		0,
		uintptr(unsafe.Pointer(&counter)),
	)
	if ret != 0 {
		return fmt.Errorf("PdhAddEnglishCounter error: %s (0x%X) for path: %s",
			GetPdhErrorText(uint32(ret)), ret, path)
	}

	q.counters[path] = counter
	return nil
}

func (q *Query) Collect() error {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	ret, _, _ := pdhCollectQueryData.Call(uintptr(q.handle))
	if ret != 0 {
		if uint32(ret) == PDH_NO_DATA {
			return fmt.Errorf("PdhCollectQueryData error: %s (0x%X): %w",
				GetPdhErrorText(uint32(ret)), ret, ErrNoData)
		}
		return fmt.Errorf("PdhCollectQueryData error: %s (0x%X)",
			GetPdhErrorText(uint32(ret)), ret)
	}
	return nil
}

func (q *Query) GetCounterValue(path string) (float64, error) {
	q.mutex.Lock()
	counter, exists := q.counters[path]
	if !exists {
		q.mutex.Unlock()
		return 0, fmt.Errorf("counter not found: %s", path)
	}
	q.mutex.Unlock()

	var counterValue PDH_FMT_COUNTERVALUE
	ret, _, _ := pdhGetFormattedCounterValue.Call(
		uintptr(counter),
		PDH_FMT_DOUBLE,
		0,
		uintptr(unsafe.Pointer(&counterValue)),
	)

	if ret != 0 {
		return 0, fmt.Errorf("PdhGetFormattedCounterValue error: %s (0x%X) for path: %s",
			GetPdhErrorText(uint32(ret)), ret, path)
	}

	if counterValue.CStatus != 0 {
		return 0, fmt.Errorf("counter status error: %s (0x%X) for path: %s",
			GetPdhErrorText(counterValue.CStatus), counterValue.CStatus, path)
	}

	logDebug("Successfully got value for %s: %f", path, counterValue.Value)
	return counterValue.Value, nil
}

func (q *Query) Close() {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	pdhCloseQuery.Call(uintptr(q.handle))
}

func BuildCounterPath(path string, instance string) string {
	if instance == "" {
		logDebug("Built path without instance: %s", path)
		return path
	}

	parts := strings.Split(path, "\\")
	if len(parts) >= 2 {
		builtPath := fmt.Sprintf("\\%s(%s)\\%s",
			parts[1],
			instance,
			strings.Join(parts[2:], "\\"))
		logDebug("Built path with instance: %s", builtPath)
		return builtPath
	}

	logDebug("Fallback path: %s", path)
	return path
}
