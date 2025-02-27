package gateway_api

import (
	"testing"

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
				Reason:             "Scheduled",
				Message:            "Shceld gageway",
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
				scheduled: true,
				msg:       "gateway not scheduled",
			},
			want: meatav1.Condition{
				Type:    "Accepted",
				Status:  "False",
				Reason:  "Not Scheduled",
				Message: "gageway is not scheduled",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gwStsAcpCondition(tt.args.gw, tt.args.scheduled, tt.args.msg)
			assert.Equal(t, tt.want, got)
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
				Message:            "gateway is ready",
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
				scheduled: true,
				msg:       "Listener Unready",
			},
			want: meatav1.Condition{
				Type:    "Ready",
				Status:  "False",
				Reason:  "Not Ready",
				Message: "gageway is not ready",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gwStsReadyCondition(tt.args.gw, tt.args.scheduled, tt.args.msg)
			assert.Equal(t, tt.want, got)
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
				Reason:             "Ready",
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
				scheduled: true,
				msg:       "Listener Unready",
			},
			want: meatav1.Condition{
				Type:    "Programmed",
				Status:  "False",
				Reason:  "Not Ready",
				Message: "UnProgrammed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gwStsProgrmCondition(tt.args.gw, tt.args.scheduled, tt.args.msg)
			assert.Equal(t, tt.want, got)
		})
	}
}
