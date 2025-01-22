const { ApolloServer, gql } = require('apollo-server');
const { PubSub } = require('graphql-subscriptions');

// Define the schema
const typeDefs = gql`
  type Query {
    hello: String
    errorQuery: String
  }

  type Mutation {
    echo(message: String!): String
  }

  type Subscription {
    messageSent: String
  }
`;

// Initialize PubSub for subscriptions
const pubSub = new PubSub();
const MESSAGE_SENT = 'MESSAGE_SENT';

// Define the resolvers
const resolvers = {
  Query: {
    hello: () => 'Hello, world!',
    errorQuery: () => {
      throw new Error('This is an error');
    },
  },
  Mutation: {
    echo: (_, { message }) => {
      pubSub.publish(MESSAGE_SENT, { messageSent: message });
      return message;
    },
  },
  Subscription: {
    messageSent: {
      subscribe: () => pubSub.asyncIterator([MESSAGE_SENT]),
    },
  },
};

// Create the Apollo Server
const server = new ApolloServer({ typeDefs, resolvers });

// Start the server
server.listen().then(({ url }) => {
  console.log(`🚀 Server ready at ${url}`);
});
