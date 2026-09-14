package gateway_api

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	meatav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func Test_gatewayStatusScheduledCondition(t *testing.T) {
	type args struct {
		gw        *gatewayv1.Gateway
		scheduled bool
		msg       string
	}
	tests := []struct {
		name string
		args args
		want meatav1.Condition
	}{
		{
			name: "schedule",
			args: args{
				gw: &gatewayv1.Gateway{
					ObjectMeta: meatav1.ObjectMeta{
						Generation: 100,
					},
				},
				scheduled: true,
				msg:       "gateway scheduled",
			},
			want: meatav1.Condition{
				Type:               "Accepted",
				Status:             "True",
				ObservedGeneration: 100,
				Reason:             "Accepted",
				Message:            "gateway scheduled",
			},
		},
		{
			name: "non-schedule",
			args: args{
				gw: &gatewayv1.Gateway{
					ObjectMeta: meatav1.ObjectMeta{
						Generation: 100,
					},
				},
				scheduled: false,
				msg:       "gateway not scheduled",
			},
			want: meatav1.Condition{
				Type:               "Accepted",
				Status:             "False",
				ObservedGeneration: 100,
				Reason:             "NoResources",
				Message:            "gateway not scheduled",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gwStsAcpCondition(tt.args.gw, tt.args.scheduled, tt.args.msg)
			assert.True(t, cmp.Equal(tt.want, got, cmpopts.IgnoreFields(meatav1.Condition{}, "LastTransitionTime")))
		})
	}
}

func Test_gatewayStatusReadyCondition(t *testing.T) {
	type args struct {
		gw        *gatewayv1.Gateway
		scheduled bool
		msg       string
	}
	tests := []struct {
		name string
		args args
		want meatav1.Condition
	}{
		{
			name: "Ready",
			args: args{
				gw: &gatewayv1.Gateway{
					ObjectMeta: meatav1.ObjectMeta{
						Generation: 100,
					},
				},
				scheduled: true,
				msg:       "Listener Ready",
			},
			want: meatav1.Condition{
				Type:               "Ready",
				Status:             "True",
				ObservedGeneration: 100,
				Reason:             "Ready",
				Message:            "Listener Ready",
			},
		},
		{
			name: "Un Ready",
			args: args{
				gw: &gatewayv1.Gateway{
					ObjectMeta: meatav1.ObjectMeta{
						Generation: 100,
					},
				},
				scheduled: false,
				msg:       "Listener Unready",
			},
			want: meatav1.Condition{
				Type:               "Ready",
				Status:             "False",
				ObservedGeneration: 100,
				Reason:             "NoResources",
				Message:            "Listener Unready",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gwStsReadyCondition(tt.args.gw, tt.args.scheduled, tt.args.msg)
			assert.True(t, cmp.Equal(tt.want, got, cmpopts.IgnoreFields(meatav1.Condition{}, "LastTransitionTime")))
		})
	}
}

func Test_gatewayListenerProgrammedConditino(t *testing.T) {
	type args struct {
		gw        *gatewayv1.Gateway
		scheduled bool
		msg       string
	}
	tests := []struct {
		name string
		args args
		want meatav1.Condition
	}{
		{
			name: "Ready",
			args: args{
				gw: &gatewayv1.Gateway{
					ObjectMeta: meatav1.ObjectMeta{
						Generation: 100,
					},
				},
				scheduled: true,
				msg:       "Programmed",
			},
			want: meatav1.Condition{
				Type:               "Programmed",
				Status:             "True",
				ObservedGeneration: 100,
				Reason:             "Accepted",
				Message:            "Programmed",
			},
		},
		{
			name: "Un Ready",
			args: args{
				gw: &gatewayv1.Gateway{
					ObjectMeta: meatav1.ObjectMeta{
						Generation: 100,
					},
				},
				scheduled: false,
				msg:       "Listener Unready",
			},
			want: meatav1.Condition{
				Type:               "Programmed",
				Status:             "False",
				ObservedGeneration: 100,
				Reason:             "ListenersNotReady",
				Message:            "Listener Unready",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gwStsProgrmCondition(tt.args.gw, tt.args.scheduled, tt.args.msg)
			assert.True(t, cmp.Equal(tt.want, got, cmpopts.IgnoreFields(meatav1.Condition{}, "LastTransitionTime")))
		})
	}
}
