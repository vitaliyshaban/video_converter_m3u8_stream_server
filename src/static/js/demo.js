document.addEventListener('DOMContentLoaded', () => {
  const source = 'http://192.168.1.149:9199/v0/b/alesiafitness-firebase.appspot.com/o/segments%2Fa73897aed686dea1403bdbaea41c7169a6aabcd9c26eb5f8c1dd5e33de291e7b%2Fa73897aed686dea1403bdbaea41c7169a6aabcd9c26eb5f8c1dd5e33de291e7b.m3u8?alt=media';
  const video = document.querySelector('video');

  http://192.168.1.149:9199/v0/b/alesiafitness-firebase.appspot.com/o/segments%2Fa73897aed686dea1403bdbaea41c7169a6aabcd9c26eb5f8c1dd5e33de291e7b%2Fa73897aed686dea1403bdbaea41c7169a6aabcd9c26eb5f8c1dd5e33de291e7b_1080_0_000.ts?alt=media
  const defaultOptions = {};

  if (!Hls.isSupported()) {
    video.src = source;
    var player = new Plyr(video, defaultOptions);
  } else {
    const hls = new Hls();
    hls.loadSource(source);
    hls.on(Hls.Events.MANIFEST_PARSED, function (event, data) {
      const availableQualities = hls.levels.map((l) => l.height)
      availableQualities.unshift(0) //prepend 0 to quality array


      hls.on(Hls.Events.LEVEL_SWITCHED, function (event, data) {
        var span = document.querySelector(".plyr__menu__container [data-plyr='quality'][value='0'] span")
        if (hls.autoLevelEnabled) {
          span.innerHTML = `AUTO (${hls.levels[data.level].height}p)`
        } else {
          span.innerHTML = `AUTO`
        }
      })

      // Initialize new Plyr player with quality options
      var player = new Plyr(video, {
        ...defaultOptions,
        debug: true,
        title: 'View From A Blue Moon',
        iconUrl: 'media/demo.svg',
        keyboard: {
          global: true,
        },
        i18n: {
          settings: 'Настройки',
          quality: 'Качество',
          speed: 'Скорость',
          normal: '1x',
          qualityLabel: {
            0: 'Auto'
          },
        },
        tooltips: {
          controls: true,
        },
        quality: {
          default: 0, //Default - AUTO
          options: availableQualities,
          forced: true,
          onChange: (e) => updateQuality(e),
        },
        fullscreen: {
          enabled: true,
          fallback: true,
          iosNative: false,
          container: '#container'
        },
        speed: {
          selected: 1,
          options: [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 4],
        },
        controls: ['play', 'progress', 'current-time', 'mute', 'volume', 'settings', 'fullscreen', 'pip', 'airplay'],
        settings: ['captions', 'quality', 'speed', 'loop'],
        captions: { active: true, update: true, language: 'en' },
        previewThumbnails: {
          enabled: true,
          src: ['https://cdn.plyr.io/static/demo/thumbs/100p.vtt', 'https://cdn.plyr.io/static/demo/thumbs/240p.vtt'],
        },
        // mediaMetadata: {
        //   title: 'View From A Blue Moon',
        //   album: 'Sports',
        //   artist: 'Brainfarm',
        //   artwork: [
        //     {
        //       src: 'https://cdn.plyr.io/static/demo/View_From_A_Blue_Moon_Trailer-HD.jpg',
        //       type: 'image/jpeg',
        //     },
        //   ],
        // },
        markers: {
          enabled: true,
          points: [
            {
              time: 10,
              label: 'first marker',
            },
            {
              time: 40,
              label: 'second marker',
            },
            {
              time: 120,
              label: '<strong>third</strong> marker',
            },
          ],
        },
      });
      player.on('ready', () => {
        hls.attachMedia(video);
      })
  
    });
    window.hls = hls;
  }

  function updateQuality(newQuality) {
    if (newQuality === 0) {
      window.hls.currentLevel = -1; //Enable AUTO quality if option.value = 0
    } else {
      window.hls.levels.forEach((level, levelIndex) => {
        if (level.height === newQuality) {
          console.log("Found quality match with " + newQuality);
          window.hls.currentLevel = levelIndex;
        }
      });
    }
  }
});