import Vue from 'vue'
import App from './app.vue'
import router from './router'
import axios from 'axios';

import 'bootstrap/dist/css/bootstrap.css'
import 'bootstrap-vue/dist/bootstrap-vue.css'
import BootstrapVue from 'bootstrap-vue'
Vue.use(BootstrapVue)

import './assets/site.css'

import VueTimeago from 'vue-timeago'
Vue.use(VueTimeago, {locale: 'en'})

Vue.config.productionTip = false
Vue.config.ignoredElements = ["video-js"]

let initialPlayerType = localStorage.getItem("playerType");
if (initialPlayerType != "RTC") {
  initialPlayerType = "HLS";
}

new Vue({
  router,
  render: h => h(App),
  data: {
    channels: {},
    user: {
      id: null,
      username: null,
      discriminator: null,
      avatar: null,
    },
    showStreamInfo: false,
    playerType: initialPlayerType,
  },
  methods: {
    updateChannels() {
      axios.get("/channels.json")
        .then(response => this.channels = response.data)
    },
    updateUser() {
      axios.get("/oauth2/user")
        .then(response => this.user = response.data)
    },
    doLogin() {
      window.location.href = "/oauth2/initiate"
    },
    doLogout() {
      axios.post("/oauth2/logout")
        .then(() => {
          this.user.id = ""
          this.updateUser()
        })
    },
    navChannel(name) {
      return {name: 'watch', params: {channel: name}}
    }
  },
  computed: {
    loggedIn() { return this.user.id !== null && this.user.id !== "" },
    liveChannels() {
      let live = new Array()
      for (let ch of Object.values(this.channels)) {
        if (ch.live) {
          live.push(ch.name)
        }
      }
      return live.sort()
    },
  },
  mounted() {
    this.updateChannels();
    this.updateUser();
    this.chinterval = window.setInterval(this.updateChannels, 5000)
    this.userinterval = window.setInterval(this.updateUser, 300000)
    this.unwatch = this.$watch("playerType", v => localStorage.setItem("playerType", v))
  },
  beforeDestroy() {
    window.clearInterval(this.chinterval)
    window.clearInterval(this.userinterval)
    this.unwatch()
  }
}).$mount('#app')
