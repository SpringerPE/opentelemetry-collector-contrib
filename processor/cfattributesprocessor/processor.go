package cfattributesprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/cfattributesprocessor"

import (
	"context"
	"strconv"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/cfattributesprocessor/internal/cf"
)

const (
	cfAttrNSPrefix = "cloudfoundry."
)

type cfAttributesProcessor struct {
	config *Config
	logger *zap.Logger
	cfCli  *cf.Client
	cancel context.CancelFunc
}

func newCFAttributesProcessor(config *Config, logger *zap.Logger) *cfAttributesProcessor {
	return &cfAttributesProcessor{
		logger: logger,
		config: config,
	}
}

// implements https://pkg.go.dev/go.opentelemetry.io/collector/component#Component  Start
func (cfap *cfAttributesProcessor) Start(ctx context.Context, host component.Host) error {
	// ctx = context.Background()
	var err error
	ctx, cfap.cancel = context.WithCancel(ctx)
	switch cfap.config.CloudFoundry.Auth.Type {
	case authTypeUserPass:
		cfap.cfCli, err = cf.New(ctx, cfap.logger, cfap.config.CloudFoundry.Endpoint,
			cf.WithCacheTTL(cfap.config.CacheTTL),
			cf.WithUserPassword(cfap.config.CloudFoundry.Auth.Username, cfap.config.CloudFoundry.Auth.Password))
	case authTypeClientCredentials:
		cfap.cfCli, err = cf.New(ctx, cfap.logger, cfap.config.CloudFoundry.Endpoint,
			cf.WithCacheTTL(cfap.config.CacheTTL),
			cf.WithClientCredentials(cfap.config.CloudFoundry.Auth.ClientID, cfap.config.CloudFoundry.Auth.ClientSecret))
	case authTypeToken:
		cfap.cfCli, err = cf.New(ctx, cfap.logger, cfap.config.CloudFoundry.Endpoint,
			cf.WithCacheTTL(cfap.config.CacheTTL),
			cf.WithToken(cfap.config.CloudFoundry.Auth.AccessToken, cfap.config.CloudFoundry.Auth.RefreshToken))
	}
	return err
}

// implements https://pkg.go.dev/go.opentelemetry.io/collector/component#Component  Shutdown
func (cfap *cfAttributesProcessor) Shutdown(ctx context.Context) error {
	cfap.cancel()
	return nil
}

func (cfap *cfAttributesProcessor) processMetrics(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	rm := md.ResourceMetrics()
	for i := 0; i < rm.Len(); i++ {
		cfap.processResource(ctx, rm.At(i).Resource())
	}
	return md, nil
}

func (cfap *cfAttributesProcessor) processLogs(ctx context.Context, ld plog.Logs) (plog.Logs, error) {
	rl := ld.ResourceLogs()
	for i := 0; i < rl.Len(); i++ {
		cfap.processResource(ctx, rl.At(i).Resource())
	}
	return ld, nil
}

func (cfap *cfAttributesProcessor) processTraces(ctx context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		cfap.processResource(ctx, rss.At(i).Resource())
	}
	return td, nil
}

func (cfap *cfAttributesProcessor) processResource(ctx context.Context, resource pcommon.Resource) error {
	done, err := cfap.processAppID(ctx, resource)
	if err != nil {
		return err
	}
	if !done {
		_, err := cfap.processSpaceID(ctx, resource)
		return err
	}
	return nil
}

func (cfap *cfAttributesProcessor) getSpaceID(resource pcommon.Resource) string {
	spaceIdentifierValue := ""
	if val, ok := resource.Attributes().Get(cfap.config.SpaceIDAttributeKeyAssociation); ok {
		if val.Type() == pcommon.ValueTypeStr {
			spaceIdentifierValue = val.Str()
		}
	}
	return spaceIdentifierValue
}

func (cfap *cfAttributesProcessor) processSpaceID(ctx context.Context, resource pcommon.Resource) (bool, error) {
	if spaceID := cfap.getSpaceID(resource); spaceID != "" {
		if cfap.config.Extract.Metadata.Space {
			err := cfap.addSpaceMetadata(spaceID, resource)
			if err != nil {
				return false, err
			}
			if cfap.config.Extract.Metadata.Org {
				orgID, err := cfap.cfCli.GetSpaceOrg(spaceID)
				if err != nil {
					return false, err
				}
				err = cfap.addOrgMetadata(orgID, resource)
				if err != nil {
					return false, err
				}
			}
			return true, nil
		}
	}
	return false, nil
}

func (cfap *cfAttributesProcessor) getAppID(resource pcommon.Resource) string {
	appIdentifierValue := ""
	if val, ok := resource.Attributes().Get(cfap.config.AppIDAttributeKeyAssociation); ok {
		if val.Type() == pcommon.ValueTypeStr {
			appIdentifierValue = val.Str()
		}
	}
	return appIdentifierValue
}

func (cfap *cfAttributesProcessor) processAppID(ctx context.Context, resource pcommon.Resource) (bool, error) {
	if appID := cfap.getAppID(resource); appID != "" {
		appName, err := cfap.cfCli.GetAppName(appID)
		if err != nil {
			return false, err
		}
		resource.Attributes().PutStr(cfAttrNSPrefix+"app.name", appName)
		if cfap.config.Extract.Metadata.App {
			if appLabels, appAnnotations, err := cfap.cfCli.GetAppMetadata(appID); err == nil {
				for k, v := range appLabels {
					resource.Attributes().PutStr(cfAttrNSPrefix+"app.labels."+k, *v)
				}
				for k, v := range appAnnotations {
					resource.Attributes().PutStr(cfAttrNSPrefix+"app.annotations."+k, *v)
				}
			} else {
				cfap.logger.Error(err.Error())
				return false, err
			}
		}
		if cfap.config.Extract.AppStateLifecycle {
			appState, err := cfap.cfCli.GetAppState(appID)
			if err != nil {
				return false, err
			}
			resource.Attributes().PutStr(cfAttrNSPrefix+"app.state", appState)
			lcType, buildpacks, stack, err := cfap.cfCli.GetAppLifecycle(appID)
			if err != nil {
				return false, err
			}
			resource.Attributes().PutStr(cfAttrNSPrefix+"app.lifecyle.type", lcType)
			resource.Attributes().PutStr(cfAttrNSPrefix+"app.lifecyle.stack", stack)
			for i, v := range buildpacks {
				resource.Attributes().PutStr(cfAttrNSPrefix+"app.lifecyle.buildpacks."+strconv.Itoa(i), v)
			}
		}
		if cfap.config.Extract.AppDates {
			created, updated, err := cfap.cfCli.GetAppDates(appID)
			if err != nil {
				return false, err
			}
			resource.Attributes().PutStr(cfAttrNSPrefix+"app.created", created)
			resource.Attributes().PutStr(cfAttrNSPrefix+"app.updated", updated)
		}
		if cfap.config.Extract.Metadata.Space {
			spaceID, err := cfap.cfCli.GetAppSpace(appID)
			if err != nil {
				return false, err
			}
			err = cfap.addSpaceMetadata(spaceID, resource)
			if err != nil {
				return false, err
			}
			if cfap.config.Extract.Metadata.Org {
				orgID, err := cfap.cfCli.GetSpaceOrg(spaceID)
				if err != nil {
					return false, err
				}
				err = cfap.addOrgMetadata(orgID, resource)
				if err != nil {
					return false, err
				}
			}
		}
		return true, nil
	}
	return false, nil
}

func (cfap *cfAttributesProcessor) addSpaceMetadata(spaceID string, resource pcommon.Resource) error {
	spaceName, err := cfap.cfCli.GetSpaceName(spaceID)
	if err != nil {
		return err
	}
	resource.Attributes().PutStr(cfAttrNSPrefix+"space.name", spaceName)
	if spaceLabels, spaceAnnotations, err := cfap.cfCli.GetSpaceMetadata(spaceID); err == nil {
		for k, v := range spaceLabels {
			resource.Attributes().PutStr(cfAttrNSPrefix+"space.labels."+k, *v)
		}
		for k, v := range spaceAnnotations {
			resource.Attributes().PutStr(cfAttrNSPrefix+"space.annotations."+k, *v)
		}
	}
}

func (cfap *cfAttributesProcessor) addOrgMetadata(orgID string, resource pcommon.Resource) error {
	orgName, err := cfap.cfCli.GetOrgName(orgID)
	if err != nil {
		return err
	}
	resource.Attributes().PutStr(cfAttrNSPrefix+"org.name", orgName)
	if orgLabels, orgAnnotations, err := cfap.cfCli.GetOrgMetadata(orgID); err == nil {
		for k, v := range orgLabels {
			resource.Attributes().PutStr(cfAttrNSPrefix+"org.labels."+k, *v)
		}
		for k, v := range orgAnnotations {
			resource.Attributes().PutStr(cfAttrNSPrefix+"org.annotations."+k, *v)
		}
	}
	return nil
}
