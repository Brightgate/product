<!--
  COPYRIGHT 2018 Brightgate Inc. All rights reserved.

  This copyright notice is Copyright Management Information under 17 USC 1202
  and is included to protect this work and deter copyright infringement.
  Removal or alteration of this Copyright Management Information without the
  express written permission of Brightgate Inc is prohibited, and any
  such unauthorized removal or alteration will be a violation of federal law.
-->
<template>
  <f7-page ptr>
    <f7-navbar :back-link="$t('message.general.back')" :title="$t('message.compliance_report.title')" sliding>
    </f7-navbar>
    <f7-card :title="$t('message.compliance_report.summary')">
      <f7-list no-hairlines no-hairlines-between>
        <f7-list-item media>
          <div slot="media">
            <f7-icon v-if="Policy_Violations !== 0" f7="bolt_round_fill" color="red"></f7-icon>
            <f7-icon v-else f7="check_round_fill" color="green"></f7-icon>
          </div>
          <span v-if="Policy_Violations !== 0" style="font-weight: bold">
            {{ $tc("message.compliance_report.summary_violations", Policy_Violations, {num: Policy_Violations}) }}
          </span>
          <span v-else>
            {{ $t("message.compliance_report.summary_no_violations") }}
          </span>
        </f7-list-item>

        <f7-list-item>
          {{ $t("message.compliance_report.summary_enrolled", {num: User_Count(All_Users)}) }}
          {{ $t("message.compliance_report.summary_phish", {num: Phishing_Incidents}) }}
        </f7-list-item>
        <f7-list-item>
          {{ $t("message.compliance_report.summary_vuln", {num: Alert_Count(Alert_Active(All_Alerts))}) }}
        </f7-list-item>
      </f7-list>
    </f7-card>

    <f7-card v-if="Alert_Count(Alert_Active(All_Alerts)) === 0"
        :title="$t('message.compliance_report.active_violations')"
        :content="$t('message.compliance_report.no_active_violations')">
    </f7-card>
    <f7-card v-else :title="$t('message.compliance_report.active_violations')">
    <f7-list>
      <f7-list-item
          v-for="alert in Alert_Active(All_Alerts)"
          :key="alert.device.uniqid + '-' + alert.vulnid"
          :link="`/devices/${alert.device.uniqid}/`">
        <span>
          <f7-icon f7="bolt_round_fill" color="red"></f7-icon>
          {{ $t('message.alerts.problem_on_device',
               {problem: vulnHeadline(alert.vulnid), device: alert.device.network_name})
          }}
        </span>
      </f7-list-item>
    </f7-list>
    </f7-card>

    <f7-card v-if="Alert_Count(Alert_Inactive(All_Alerts)) === 0"
        :title="$t('message.compliance_report.resolved_violations')"
        :content="$t('message.compliance_report.no_resolved_violations')">
    </f7-card>
    <f7-card v-else :title="$t('message.compliance_report.resolved_violations')">
      <f7-list>
        <f7-list-item
            v-for="alert in Alert_Inactive(All_Alerts)"
            :key="alert.device.uniqid + '-' + alert.vulnid">
          <span>
            <f7-icon f7="bolt_round_fill" color="gray"></f7-icon>
            {{ $t('message.alerts.problem_on_device',
                 {problem: vulnHeadline(alert.vulnid), device: alert.device.network_name})
            }}
          </span>
        </f7-list-item>
      </f7-list>
    </f7-card>

    <f7-card :title="$t('message.compliance_report.ring_summary')">
      <f7-block style="margin-top: 5px; font-size: 12pt;">
        <span style="color: rgba(0,0,0,0.5);">
          <f7-icon f7="check_round_fill" size="1em" color="green"/>
            {{ $t('message.compliance_report.ring_ok') }}<br />
          <f7-icon f7="help_fill" size="1em" color="orange"/>
            {{ $t('message.compliance_report.ring_not_scanned') }}<br />
          <f7-icon f7="bolt_round_fill" size="1em" color="red"/>
            {{ $t('message.compliance_report.ring_vulnerable') }}<br />
          <f7-icon f7="circle" size="1em" color="gray"/>
            {{ $t('message.compliance_report.ring_inactive') }}<br />
          <br />
        </span>

        <f7-row style="padding-top: 7px; padding-bottom: 7px;">
          <f7-col width="40">
            <!-- <f7-icon f7="data_fill" color="white"></f7-icon> -->
            {{ $t('message.compliance_report.population') }}
          </f7-col>
          <f7-col width="60" style="text-align: center">
            <bg-ring-summary :devices="All_Devices" show-zero />
          </f7-col>
        </f7-row>

        <f7-row v-for="ring in SortedRings"
            :key="ring"
            style="padding-top: 7px; padding-bottom: 7px;">
          <f7-col width="40">
            {{ ring }}
          </f7-col>
          <f7-col width="60" style="text-align: center">
            <bg-ring-summary :devices="Devices_By_Ring(ring)" />
          </f7-col>
        </f7-row>
      </f7-block>
    </f7-card>

  </f7-page>
</template>

<script>
import Vuex from 'vuex';
import Promise from 'bluebird';
import _ from 'lodash';
import vulnerability from '../vulnerability';
import BGRingSummary from '../components/ring_summary.vue';

export default {
  data: function() {
    return {
    };
  },

  components: {
    'bg-ring-summary': BGRingSummary,
  },

  computed: {
    // Map various $store elements as computed properties for use in the
    // template.
    ...Vuex.mapGetters([
      'Mock',
      'Is_Logged_In',
      'All_Devices',
      'Device_Count',
      'Device_VulnScanned',
      'Device_Vulnerable',
      'Device_NotVulnerable',
      'Device_Active',
      'Devices_By_Ring',
      'Network_Config',
      'All_Alerts',
      'Alert_Active',
      'Alert_Count',
      'Alert_Inactive',
      'All_Users',
      'User_Count',
      'Rings',
    ]),
    Phishing_Incidents: function() {
      return 0;
    },
    Policy_Violations: function() {
      return this.Alert_Count(this.Alert_Active(this.All_Alerts));
    },
    SortedRings: function() {
      return _.sortBy(this.Rings, (r) => {
        return -1 * _.size(this.Devices_By_Ring(r));
      });
    },
  },

  methods: {
    vulnHeadline: function(vulnid) {
      return vulnerability.headline(vulnid);
    },
  },

  on: {
    'ptr:refresh': function(el, done) {
      return Promise.all([
        this.$store.dispatch('fetchNetworkConfig').catch(() => {}),
        this.$store.dispatch('fetchDevices').catch(() => {}),
        this.$store.dispatch('fetchRings').catch(() => {}),
      ]).asCallback(done);
    },

    'pageBeforeIn': function() {
      this.$store.dispatch('fetchDevices').catch(() => {});
      this.$store.dispatch('fetchRings').catch(() => {});
    },
  },
};
</script>

