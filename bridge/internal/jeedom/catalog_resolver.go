package jeedom

import (
	"sort"
	"strings"

	"github.com/RCooLeR/AjaxBridge/internal/devicecatalog"
)

type CatalogResolverConfig struct {
	Account          string
	AccountNames     []string
	DiscoverUnlinked bool
}

type CatalogResolver struct {
	cfg            CatalogResolverConfig
	byCommandID    map[string]DeviceIdentity
	byDeviceName   map[string]DeviceIdentity
	ambiguousNames map[string]struct{}
	accountNames   map[string]struct{}
	account        DeviceIdentity
}

func NewCatalogResolver(catalog *devicecatalog.Catalog, cfg CatalogResolverConfig) *CatalogResolver {
	resolver := &CatalogResolver{
		cfg:            cfg,
		byCommandID:    make(map[string]DeviceIdentity),
		byDeviceName:   make(map[string]DeviceIdentity),
		ambiguousNames: make(map[string]struct{}),
		accountNames:   make(map[string]struct{}),
	}
	if catalog == nil {
		return resolver
	}

	devices := catalog.Devices()
	accounts := uniqueAccounts(devices)
	account := strings.TrimSpace(cfg.Account)
	if account == "" && len(accounts) == 1 {
		account = accounts[0]
	}
	if account != "" {
		resolver.account = DeviceIdentity{
			DeviceSlug:     "account_" + Slug(account),
			DeviceName:     "Ajax account " + account,
			BaseSlug:       "account_" + Slug(account),
			HAIdentifiers:  []string{"ajaxbridge_account_" + account},
			HAManufacturer: "Ajax Systems",
			HAModel:        "Ajax account",
			LinkedSource:   "sia",
			LinkedAccount:  account,
		}
	}
	for _, name := range cfg.AccountNames {
		key := aliasKey(name)
		if key != "" {
			resolver.accountNames[key] = struct{}{}
		}
	}

	for _, device := range devices {
		identity := identityForCatalogDevice(device)
		for _, commandID := range device.JeedomCommandIDs {
			commandID = strings.TrimSpace(commandID)
			if commandID != "" {
				resolver.byCommandID[commandID] = identity
			}
		}
		for _, alias := range catalogDeviceAliases(device) {
			key := aliasKey(alias)
			if key == "" {
				continue
			}
			if _, ambiguous := resolver.ambiguousNames[key]; ambiguous {
				continue
			}
			if existing, exists := resolver.byDeviceName[key]; exists && existing.DeviceSlug != identity.DeviceSlug {
				// A shared display name cannot establish ownership. Keep it
				// unresolved regardless of catalog order; explicit command IDs
				// still identify each physical device or logical App separately.
				delete(resolver.byDeviceName, key)
				resolver.ambiguousNames[key] = struct{}{}
				continue
			}
			resolver.byDeviceName[key] = identity
		}
	}
	return resolver
}

func (r *CatalogResolver) Resolve(evt Event, _ Mapping) DeviceIdentity {
	if r == nil {
		return DeviceIdentity{}
	}
	if identity, ok := r.byCommandID[evt.CommandID]; ok {
		return identity
	}
	if _, ambiguous := r.ambiguousNames[aliasKey(evt.DeviceName)]; ambiguous {
		return DeviceIdentity{DiscoveryDisabled: !r.cfg.DiscoverUnlinked}
	}
	if identity, ok := r.byDeviceName[aliasKey(evt.DeviceName)]; ok {
		return identity
	}
	if _, ok := r.accountNames[aliasKey(evt.DeviceName)]; ok && r.account.DeviceSlug != "" && isHubCommand(evt.CommandName) {
		return r.account
	}
	return DeviceIdentity{
		DiscoveryDisabled: !r.cfg.DiscoverUnlinked,
	}
}

func (r *CatalogResolver) ResolveDiscovery(discovery Discovery) DeviceIdentity {
	if r == nil {
		return DeviceIdentity{}
	}
	commandIDs := make([]string, 0, len(discovery.InfoCommands)+len(discovery.Actions))
	for _, command := range discovery.InfoCommands {
		commandIDs = append(commandIDs, command.CommandID)
	}
	for _, command := range discovery.Actions {
		commandIDs = append(commandIDs, command.CommandID)
	}
	sort.Strings(commandIDs)
	for _, commandID := range commandIDs {
		if identity, ok := r.byCommandID[commandID]; ok {
			return identity
		}
	}
	if _, ambiguous := r.ambiguousNames[aliasKey(discovery.Name)]; ambiguous {
		return DeviceIdentity{DiscoveryDisabled: !r.cfg.DiscoverUnlinked}
	}
	if identity, ok := r.byDeviceName[aliasKey(discovery.Name)]; ok {
		return identity
	}
	if _, ok := r.accountNames[aliasKey(discovery.Name)]; ok && r.account.DeviceSlug != "" && isHubDeviceType(discovery.DeviceType) {
		return r.account
	}
	if r.account.DeviceSlug != "" && isHubDeviceType(discovery.DeviceType) {
		return r.account
	}
	return DeviceIdentity{
		DiscoveryDisabled: !r.cfg.DiscoverUnlinked,
	}
}

func identityForCatalogDevice(device devicecatalog.Device) DeviceIdentity {
	name := RepairText(device.Name)
	room := RepairText(device.Room)
	kind := strings.TrimSpace(device.Kind)
	if isCatalogAccountDevice(device) {
		account := strings.TrimSpace(device.Account)
		return DeviceIdentity{
			DeviceSlug:     "account_" + Slug(account),
			DeviceName:     firstNonEmpty(name, "Ajax account "+account),
			BaseSlug:       "account_" + Slug(account),
			HAIdentifiers:  []string{"ajaxbridge_account_" + account},
			HAManufacturer: "Ajax Systems",
			HAModel:        firstNonEmpty(kind, "Ajax account"),
			SuggestedArea:  room,
			LinkedSource:   "sia",
			LinkedAccount:  account,
		}
	}
	return DeviceIdentity{
		DeviceSlug:     "sia_" + Slug(device.Account) + "_zone_" + Slug(device.Zone),
		DeviceName:     firstNonEmpty(name, "Ajax zone "+device.Zone),
		BaseSlug:       "sia_" + Slug(device.Account) + "_zone_" + Slug(device.Zone),
		HAIdentifiers:  []string{"ajaxbridge_" + device.Account + "_zone_" + device.Zone},
		HAManufacturer: "Ajax Systems",
		HAModel:        firstNonEmpty(kind, "Ajax device"),
		SuggestedArea:  room,
		LinkedSource:   "sia",
		LinkedAccount:  device.Account,
		LinkedZone:     device.Zone,
	}
}

func isCatalogAccountDevice(device devicecatalog.Device) bool {
	if strings.TrimSpace(device.Account) == "" || strings.TrimSpace(device.Zone) != "" {
		return false
	}
	switch commandKey(firstNonEmpty(device.Kind, device.Device, device.Name)) {
	case "account", "ajaxaccount", "hub", "hub2", "hub2plus", "hub_2_plus", "hubplus", "hub_plus", "hubhybrid", "hub_hybrid":
		return true
	default:
		return false
	}
}

func catalogDeviceAliases(device devicecatalog.Device) []string {
	aliases := make([]string, 0, len(device.JeedomNames)+1)
	aliases = append(aliases, device.Name)
	aliases = append(aliases, device.JeedomNames...)
	return aliases
}

func aliasKey(value string) string {
	value = RepairText(value)
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return Slug(value)
}

func uniqueAccounts(devices []devicecatalog.Device) []string {
	seen := make(map[string]struct{})
	for _, device := range devices {
		if strings.TrimSpace(device.Account) != "" {
			seen[device.Account] = struct{}{}
		}
	}
	accounts := make([]string, 0, len(seen))
	for account := range seen {
		accounts = append(accounts, account)
	}
	sort.Strings(accounts)
	return accounts
}

func isHubCommand(command string) bool {
	switch commandKey(command) {
	case "etat", "sourceevenement", "evenement", "codeevenement", "trafique", "gsm", "cms", "ethernet", "alimentationsecteur", "etatdelabatterie", "batterie", "intensitedusignalcellulaire", "typereseaugsm", "donneescellulairesactives":
		return true
	default:
		return false
	}
}

func isHubDeviceType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "hub", "hub_2_plus", "hub2plus", "hub_plus", "hubplus":
		return true
	default:
		return false
	}
}
