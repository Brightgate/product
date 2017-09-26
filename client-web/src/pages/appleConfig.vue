<template>
  <!-- App -->
  <div id="app">

    <!-- Statusbar -->
    <f7-statusbar></f7-statusbar>

    <!-- Main Views -->
    <f7-views>
      <f7-view id="main-view" navbar-through :dynamic-navbar="true" main>
        <!-- Pages -->
        <f7-pages>
          <f7-page>
            <f7-block-title>Brightgate iOS Device Enrollment</f7-block-title>
            <f7-block inner>
              <p>Your iOS device will be automatically configured.  Blah blah blah.</p>
            </f7-block>
            <f7-list>
              <f7-list-item link="/appleConfigHelp/" title="Help"></f7-list-item>
            </f7-list>
            <f7-button active external href="/appleConnect">Join Network</f7-button>
          </f7-page>
        </f7-pages>
      </f7-view>
    </f7-views>

  </div>
</template>

<script>
export default {}
</script>
