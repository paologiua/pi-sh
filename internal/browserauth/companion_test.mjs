import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import test from 'node:test';
import vm from 'node:vm';

const source = await readFile(new URL('./companion.js', import.meta.url), 'utf8');

test('HTTP 200 reauthentication HTML reports required sign-in before ICE setup', async () => {
  const origin = 'https://connect.raspberrypi.com';
  const base = 'http://127.0.0.1:12345/test-capability/';
  const reauthHTML = '<html><head><title>Reauthenticating with Raspberry Pi ID</title></head><body><form action="/auth/raspberry_pi" method="post"></form></body></html>';
  let failure;
  let configurationPosted = false;
  const calls = [];
  // Minimal DOM contracts at the real companion call site: a Devices page,
  // followed by valid HTML containing an auth form and no shell controller.
  const document = {
    querySelector: () => null,
    querySelectorAll: () => [{href: origin + '/devices/deadbeef', textContent: 'test-device'}]
  };
  class DOMParser {
    parseFromString(html, type) {
      assert.equal(html, reauthHTML);
      assert.equal(type, 'text/html');
      return {
        querySelector: () => null,
        forms: [{getAttribute: name => name === 'action' ? '/auth/raspberry_pi' : null}]
      };
    }
  }
  const fetch = async (url, options = {}) => {
    calls.push(String(url));
    if (String(url) === origin + '/devices/deadbeef/remote-shell-session') {
      return new Response(reauthHTML, {status: 200, headers: {'Content-Type': 'text/html'}});
    }
    if (String(url) === base + 'failure') {
      failure = JSON.parse(options.body);
      return new Response(null, {status: 204});
    }
    if (String(url) === base + 'configuration') configurationPosted = true;
    throw new Error('Unexpected test request');
  };
  const context = vm.createContext({document, DOMParser, fetch, URL, AbortSignal, location: {origin, href: origin + '/devices'}});
  const companion = vm.runInContext('(' + source + ')', context);
  await companion(base, 'test-device');
  assert.equal(failure?.code, 'reauthentication_required');
  assert.equal(configurationPosted, false);
  assert.equal(calls.some(url => url.endsWith('/connections')), false);
});

async function runCSRFScenario({onShellPage = false, pageToken = 'fresh-fixture-token', refuse = false, offers = 1} = {}) {
  const origin = 'https://connect.raspberrypi.com';
  const base = 'http://127.0.0.1:12345/test-capability/';
  const shell = {getAttribute: name => name === 'data-shell-device-value' ? JSON.stringify({id:'deadbeef', name:'test-device'}) : null};
  const selector = token => name => name === '[data-controller~="shell"]' ? shell : name === 'meta[name="csrf-token"]' && token ? {content:token} : null;
  const document = {
    forms:[],
    querySelector: name => name === '[data-controller~="shell"]' && !onShellPage ? null : selector('document-fixture-token')(name),
    querySelectorAll: () => [{href:origin+'/devices/deadbeef', textContent:'test-device'}]
  };
  class DOMParser {parseFromString(){return {forms:[], querySelector:selector(pageToken)};}}
  const expected = onShellPage || !pageToken ? 'document-fixture-token' : pageToken;
  const state = {creates:0, answerPosted:false, answers:0, offersServed:0, alive:0, header:undefined, failure:undefined};
  const fetch = async (url, options={}) => {
    url=String(url);
    if(url===origin+'/devices/deadbeef/remote-shell-session')return new Response('<shell-fixture>');
    if(url===origin+'/devices/deadbeef/ice-configuration')return Response.json({iceServers:[]});
    if(url===base+'configuration')return new Response(null,{status:204});
    // The CLI asks for one offer per Connect connection, then is gone (410).
    if(url===base+'offer')return state.offersServed++<offers?Response.json({type:'offer',sdp:'fixture-only'}):new Response(null,{status:410});
    if(url===origin+'/devices/deadbeef/connections') {
      state.creates++;
      state.header=options.headers['X-CSRF-Token'];
      if(refuse || state.header!==expected)return new Response(null,{status:422});
      return new Response(null,{status:201,headers:{Location:'/devices/deadbeef/connections/fixture'}});
    }
    if(url===origin+'/devices/deadbeef/connections/fixture')return Response.json({answer:{type:'answer',sdp:'fixture-only'}});
    if(url===base+'answer'){state.answerPosted=true;state.answers++;return new Response(null,{status:204});}
    // Stop the heartbeat after setup. No wall-clock sleep or background task.
    // Alive until the CLI has every answer it asked for; then it exits (410).
    if(url===base+'alive'){state.alive++;return new Response(null,{status:state.answers<offers?204:410});}
    if(url===origin+'/devices/deadbeef.json')return new Response(null,{status:204});
    if(url===base+'authorized')return new Response(null,{status:204});
    if(url===base+'failure'){state.failure=JSON.parse(options.body).code;return new Response(null,{status:204});}
    throw new Error('Unexpected fixture request');
  };
  const context=vm.createContext({document,DOMParser,fetch,URL,AbortSignal,setTimeout: callback=>{callback();return 0;},location:{origin,href:origin+(onShellPage?'/devices/deadbeef/remote-shell-session':'/devices')}});
  await vm.runInContext('('+source+')',context)(base,'test-device');
  return state;
}

test('a fresh shell page supplies CSRF after the Devices page becomes stale',async()=>{
  const state=await runCSRFScenario();
  assert.equal(state.header,'fresh-fixture-token');
  assert.equal(state.answerPosted,true);
  assert.equal(state.creates,1);
});

test('an existing shell page and missing fetched metadata retain the document token',async()=>{
  for(const options of [{onShellPage:true},{pageToken:null}]) {
    const state=await runCSRFScenario(options);
    assert.equal(state.header,'document-fixture-token');
    assert.equal(state.answerPosted,true);
    assert.equal(state.creates,1);
  }
});

test('a refused connection is reported without replaying the create request',async()=>{
  const state=await runCSRFScenario({refuse:true});
  assert.equal(state.creates,1);
  assert.equal(state.answerPosted,false);
  assert.equal(state.failure,'connect_request_failed');
});

test('each further CLI offer gets its own Connect connection and a single heartbeat runs',async()=>{
  const state=await runCSRFScenario({offers:2});
  assert.equal(state.creates,2);
  assert.equal(state.answers,2);
  assert.equal(state.failure,'local_companion_failed');
});
