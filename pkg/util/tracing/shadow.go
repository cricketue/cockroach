// Copyright 2017 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.
//
// Author: Radu Berinde (radu@cockroachlabs.com)
//
// A "shadow" tracer can be any opentracing.Tracer implementation that is used
// in addition to the normal functionality of our tracer. It works by attaching
// a shadow span to every span, and attaching a shadow context to every span
// context. When injecting a span context, we encapsulate the shadow context
// inside ours.

package tracing

import (
	"github.com/cockroachdb/cockroach/pkg/settings"
	lightstep "github.com/lightstep/lightstep-tracer-go"
	opentracing "github.com/opentracing/opentracing-go"
)

type shadowTracerManager interface {
	Name() string
	Close(tr opentracing.Tracer)
}

type lightStepManager struct{}

func (lightStepManager) Name() string {
	return "lightstep"
}

func (lightStepManager) Close(tr opentracing.Tracer) {
	// TODO(radu): these calls are not reliable. FlushLightstepTracer exits
	// immediately if a flush is in progress (see
	// github.com/lightstep/lightstep-tracer-go/issues/89), and CloseTracer always
	// exits immediately (see
	// https://github.com/lightstep/lightstep-tracer-go/pull/85#discussion_r123800322).
	_ = lightstep.FlushLightStepTracer(tr)
	_ = lightstep.CloseTracer(tr)
}

type shadowTracer struct {
	opentracing.Tracer
	manager shadowTracerManager
}

func (st *shadowTracer) Typ() string {
	return st.manager.Name()
}

func (st *shadowTracer) Close() {
	st.manager.Close(st)
}

// linkShadowSpan creates and links a Shadow span to the passed-in span (i.e.
// fills in s.shadowTr and s.shadowSpan). This should only be called when
// shadow tracing is enabled.
//
// The Shadow span will have a parent if parentShadowCtx is not nil.
// parentType is ignored if parentShadowCtx is nil.
//
// The tags from s are copied to the Shadow span.
func linkShadowSpan(
	s *span,
	shadowTr *shadowTracer,
	parentShadowCtx opentracing.SpanContext,
	parentType opentracing.SpanReferenceType,
) {
	// Create the shadow lightstep span.
	var opts []opentracing.StartSpanOption
	// Replicate the options, using the lightstep context in the reference.
	opts = append(opts, opentracing.StartTime(s.startTime))
	if s.mu.tags != nil {
		opts = append(opts, s.mu.tags)
	}
	if parentShadowCtx != nil {
		opts = append(opts, opentracing.SpanReference{
			Type:              parentType,
			ReferencedContext: parentShadowCtx,
		})
	}
	s.shadowTr = shadowTr
	s.shadowSpan = shadowTr.StartSpan(s.operation, opts...)
}

var lightStepToken = settings.RegisterStringSetting(
	"trace.lightstep.token",
	"if set, traces go to Lightstep using this token",
	"",
)

func createLightStepTracer(token string) opentracing.Tracer {
	return lightstep.NewTracer(lightstep.Options{
		AccessToken:      token,
		MaxLogsPerSpan:   maxLogsPerSpan,
		MaxBufferedSpans: 10000,
		UseGRPC:          true,
	})
}

// We don't call OnChange inline above because it causes an "initialization
// loop" compile error.
var _ = lightStepToken.OnChange(updateShadowTracers)

func updateShadowTracer(t *Tracer) {
	if lsToken := lightStepToken.Get(); lsToken != "" {
		t.setShadowTracer(lightStepManager{}, createLightStepTracer(lsToken))
	} else {
		t.setShadowTracer(nil, nil)
	}
}

func updateShadowTracers() {
	tracerRegistry.ForEach(updateShadowTracer)
}
