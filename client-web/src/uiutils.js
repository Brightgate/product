/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */
import assert from 'assert';

import Debug from 'debug';

import siteApi from './api/site';

const debug = Debug('uiutils');

async function submitConfigChange(component, description, storeDispatchOp, storeDispatchArg, errMsgFunc) {
  assert.equal(typeof description, 'string');
  assert.equal(typeof storeDispatchOp, 'string');
  assert.equal(typeof storeDispatchArg, 'object');
  assert.equal(typeof errMsgFunc, 'function');

  debug(`submitting config change: ${description} using store op ${storeDispatchOp}`);
  let res;

  try {
    component.$f7.preloader.show();
    res = await component.$store.dispatch(storeDispatchOp, storeDispatchArg);
  } catch (err) {
    debug(`${description} failed`, err);
    let txt;
    if (err instanceof siteApi.UnfinishedOperationError) {
      txt = component.$t('message.api.unfinished_operation');
    } else {
      txt = errMsgFunc(err);
    }
    component.$f7.toast.show({
      text: txt,
      closeButton: true,
      destroyOnClose: true,
    });
  } finally {
    component.$f7.preloader.hide();
  }
  return res;
}

function formatNodeName(component, nodes, nodeName) {
  if (!nodes[nodeName] || !nodes[nodeName].name) {
    return component.$t('message.api.unknown_device', {id: nodeName});
  }
  return nodes[nodeName].name;
}

function dBmToStrength(dBm) {
  if (typeof dBm !== 'number') {
    return 0;
  }
  if (dBm > -50) {
    return 5;
  } else if (dBm > -60) {
    return 4;
  } else if (dBm > -70) {
    return 3;
  } else if (dBm > -80) {
    return 2;
  }
  return 1;
}

export default {
  submitConfigChange,
  formatNodeName,
  dBmToStrength,
};
